package hotwordtrain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultModelName        = "sloppy"
	defaultGeneratedSamples = 250
	defaultPreferredModel   = "qwen3tts"
	recordingsDirName       = "recordings"
	modelsDirName           = "models"
	generatedDirName        = "generated"
	settingsFileName        = "settings.json"
	statusStateIdle         = "idle"
	statusStateRunning      = "running"
	statusStateCompleted    = "completed"
	statusStateFailed       = "failed"
	recordingKindHotword    = "hotword"
	recordingKindReference  = "reference"
	recordingKindTest       = "test"
	maxStatusProgress       = 100
)

type Manager struct {
	dataDir      string
	projectRoot  string
	trainingPath string

	mu             sync.Mutex
	generatorPaths map[string]string
	settings       Settings
	generation     Status
	training       Status
	generationSubs map[chan Status]struct{}
	trainingSubs   map[chan Status]struct{}
}

func New(dataDir, projectRoot string) *Manager {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		root = "."
	}
	root, _ = filepath.Abs(root)
	dataDir, _ = filepath.Abs(strings.TrimSpace(dataDir))
	manager := &Manager{
		dataDir:      dataDir,
		projectRoot:  root,
		trainingPath: filepath.Join(root, "scripts", "train-hotword.sh"),
		generatorPaths: map[string]string{
			"qwen3tts":  defaultGeneratorPath(strings.TrimSpace(os.Getenv("TABURA_HOTWORD_QWEN3TTS_COMMAND")), filepath.Join(root, "scripts", "hotword-generate-qwen3tts.sh")),
			"gptsovits": defaultGeneratorPath(strings.TrimSpace(os.Getenv("TABURA_HOTWORD_GPTSOVITS_COMMAND")), filepath.Join(root, "scripts", "hotword-generate-gptsovits.sh")),
			"piper":     filepath.Join(root, "scripts", "hotword-generate-piper.sh"),
			"kokoro":    defaultGeneratorPath(strings.TrimSpace(os.Getenv("TABURA_HOTWORD_KOKORO_COMMAND")), filepath.Join(root, "scripts", "hotword-generate-kokoro.sh")),
		},
		generationSubs: make(map[chan Status]struct{}),
		trainingSubs:   make(map[chan Status]struct{}),
	}
	manager.settings = defaultSettings()
	manager.loadSettings()
	manager.generation = Status{State: statusStateIdle, Stage: "idle"}
	manager.training = Status{State: statusStateIdle, Stage: "idle"}
	return manager
}

func (m *Manager) SetTrainingScriptPath(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trainingPath = strings.TrimSpace(path)
}

func (m *Manager) SetGeneratorScriptPath(model, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.generatorPaths == nil {
		m.generatorPaths = make(map[string]string)
	}
	m.generatorPaths[normalizeModelID(model)] = strings.TrimSpace(path)
}

func (m *Manager) GenerationStatus() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneStatus(m.generation)
}

func (m *Manager) TrainingStatus() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneStatus(m.training)
}

func (m *Manager) WatchGeneration() (<-chan Status, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan Status, 1)
	m.generationSubs[ch] = struct{}{}
	ch <- cloneStatus(m.generation)
	return ch, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.generationSubs, ch)
		close(ch)
	}
}

func (m *Manager) WatchTraining() (<-chan Status, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan Status, 1)
	m.trainingSubs[ch] = struct{}{}
	ch <- cloneStatus(m.training)
	return ch, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.trainingSubs, ch)
		close(ch)
	}
}

func (m *Manager) StartGeneration(ctx context.Context, req GenerateRequest) error {
	models := normalizeModels(req.Models)
	if len(models) == 0 {
		return errors.New("at least one model is required")
	}
	sampleCount := req.SampleCount
	if sampleCount <= 0 {
		sampleCount = defaultGeneratedSamples
	}

	m.mu.Lock()
	if m.generation.State == statusStateRunning {
		m.mu.Unlock()
		return errors.New("generation already running")
	}
	modelStates := make([]ModelStatus, 0, len(models))
	for _, model := range models {
		modelStates = append(modelStates, ModelStatus{
			Name:   model,
			State:  statusStateIdle,
			Target: sampleCount,
		})
	}
	m.setGenerationLocked(Status{
		State:     statusStateRunning,
		Stage:     "queued",
		Message:   "Queued generation job.",
		Progress:  1,
		StartedAt: nowRFC3339(),
		UpdatedAt: nowRFC3339(),
		Models:    modelStates,
	})
	m.mu.Unlock()

	go m.runGeneration(ctx, models, sampleCount)
	return nil
}

func (m *Manager) StartTraining(ctx context.Context, req TrainRequest) error {
	m.mu.Lock()
	if m.training.State == statusStateRunning {
		m.mu.Unlock()
		return errors.New("training already running")
	}
	m.setTrainingLocked(Status{
		State:     statusStateRunning,
		Stage:     "queued",
		Message:   "Queued training job.",
		Progress:  1,
		StartedAt: nowRFC3339(),
		UpdatedAt: nowRFC3339(),
	})
	m.mu.Unlock()

	go m.runTraining(ctx, req)
	return nil
}

func (m *Manager) recordingsDir() string {
	return filepath.Join(m.dataDir, "hotword-train", recordingsDirName)
}

func (m *Manager) modelsDir() string {
	return filepath.Join(m.dataDir, "hotword-train", modelsDirName)
}

func (m *Manager) generatedDir() string {
	return filepath.Join(m.dataDir, "hotword-train", generatedDirName)
}

func (m *Manager) settingsPath() string {
	return filepath.Join(m.dataDir, "hotword-train", settingsFileName)
}

func (m *Manager) vendorModelPath() string {
	return filepath.Join(m.projectRoot, "internal", "web", "static", "vendor", "openwakeword", defaultModelName+".onnx")
}

func cloneStatus(status Status) Status {
	out := status
	if len(status.Models) > 0 {
		out.Models = append([]ModelStatus(nil), status.Models...)
	}
	return out
}

func defaultGeneratorPath(configured, fallback string) string {
	if strings.TrimSpace(configured) != "" {
		return strings.TrimSpace(configured)
	}
	return fallback
}

func (m *Manager) setGenerationLocked(status Status) {
	status.Progress = clampProgress(status.Progress)
	if status.UpdatedAt == "" {
		status.UpdatedAt = nowRFC3339()
	}
	m.generation = cloneStatus(status)
	for ch := range m.generationSubs {
		sendLatestStatus(ch, m.generation)
	}
}

func (m *Manager) setTrainingLocked(status Status) {
	status.Progress = clampProgress(status.Progress)
	if status.UpdatedAt == "" {
		status.UpdatedAt = nowRFC3339()
	}
	m.training = cloneStatus(status)
	for ch := range m.trainingSubs {
		sendLatestStatus(ch, m.training)
	}
}

func sendLatestStatus(ch chan Status, status Status) {
	select {
	case ch <- cloneStatus(status):
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- cloneStatus(status):
	default:
	}
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func clampProgress(progress int) int {
	if progress < 0 {
		return 0
	}
	if progress > maxStatusProgress {
		return maxStatusProgress
	}
	return progress
}

func randomID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

func normalizeKind(kind string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case recordingKindReference:
		return recordingKindReference
	case recordingKindTest:
		return recordingKindTest
	default:
		return recordingKindHotword
	}
}

func normalizeModelID(model string) string {
	switch strings.TrimSpace(strings.ToLower(model)) {
	case "qwen3tts", "qwen3-tts":
		return "qwen3tts"
	case "gpt-sovits", "gptsovits":
		return "gptsovits"
	case "kokoro", "kokoro-82m":
		return "kokoro"
	default:
		return "piper"
	}
}

func normalizeModels(models []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(models))
	for _, model := range models {
		clean := normalizeModelID(model)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}

func runLoggedCommand(ctx context.Context, cmd *exec.Cmd, onLine func(string)) error {
	pipeReader, pipeWriter := io.Pipe()
	cmd.Stdout = pipeWriter
	cmd.Stderr = pipeWriter
	lineErrCh := make(chan error, 1)

	go func() {
		defer close(lineErrCh)
		defer pipeReader.Close()
		buffer := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, err := pipeReader.Read(tmp)
			if n > 0 {
				buffer = append(buffer, tmp[:n]...)
				for {
					idx := -1
					for i, b := range buffer {
						if b == '\n' {
							idx = i
							break
						}
					}
					if idx < 0 {
						break
					}
					line := strings.TrimSpace(string(buffer[:idx]))
					if line != "" && onLine != nil {
						onLine(line)
					}
					buffer = buffer[idx+1:]
				}
			}
			if err != nil {
				if len(buffer) > 0 && onLine != nil {
					line := strings.TrimSpace(string(buffer))
					if line != "" {
						onLine(line)
					}
				}
				if errors.Is(err, io.EOF) {
					lineErrCh <- nil
				} else {
					lineErrCh <- err
				}
				return
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		_ = pipeWriter.Close()
		return err
	}
	waitErr := cmd.Wait()
	_ = pipeWriter.Close()
	if readErr := <-lineErrCh; readErr != nil {
		return readErr
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return waitErr
}

func newestModel(models []Model) string {
	if len(models) == 0 {
		return ""
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].CreatedAt > models[j].CreatedAt
	})
	return models[0].FileName
}

func sortModels(models []Model) {
	sort.Slice(models, func(i, j int) bool {
		if models[i].Production != models[j].Production {
			return models[i].Production
		}
		if models[i].CreatedAt != models[j].CreatedAt {
			return models[i].CreatedAt > models[j].CreatedAt
		}
		return models[i].FileName < models[j].FileName
	})
}

func countWAVFiles(root string) int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".wav") {
			count++
		}
	}
	return count
}

func (m *Manager) ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func metadataPathForAudio(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".json"
}

func (m *Manager) generatorPath(model string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.TrimSpace(m.generatorPaths[normalizeModelID(model)])
}

func defaultNegativePhrases() []string {
	return []string{
		"copy",
		"happy",
		"poppy",
		"soapy",
		"sleepy",
		"slowly",
		"sorry",
		"soppy",
		"stocky",
		"rocky",
	}
}

func defaultSettings() Settings {
	return Settings{
		PreferredGenerator: defaultPreferredModel,
		SampleCount:        2000,
		AutoDeploy:         true,
		NegativePhrases:    defaultNegativePhrases(),
		GeneratorCommands:  map[string]string{},
	}
}

func (m *Manager) loadSettings() {
	path := m.settingsPath()
	var stored Settings
	if err := decodeJSONFile(path, &stored); err != nil {
		return
	}
	m.applySettingsLocked(stored)
}

func (m *Manager) persistSettingsLocked() error {
	if err := m.ensureDir(filepath.Dir(m.settingsPath())); err != nil {
		return err
	}
	return writeJSONFile(m.settingsPath(), m.settings)
}

func normalizeNegativePhrases(values []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
		clean := strings.TrimSpace(strings.ToLower(value))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	if len(out) == 0 {
		return defaultNegativePhrases()
	}
	sort.Strings(out)
	return out
}

func (m *Manager) applySettingsLocked(stored Settings) {
	if m.settings.GeneratorCommands == nil {
		m.settings.GeneratorCommands = map[string]string{}
	}
	preferred := normalizeModelID(stored.PreferredGenerator)
	if preferred == "" {
		preferred = defaultPreferredModel
	}
	sampleCount := stored.SampleCount
	if sampleCount <= 0 {
		sampleCount = defaultSettings().SampleCount
	}
	m.settings.PreferredGenerator = preferred
	m.settings.SampleCount = sampleCount
	m.settings.AutoDeploy = stored.AutoDeploy
	m.settings.NegativePhrases = normalizeNegativePhrases(stored.NegativePhrases)
	if len(stored.GeneratorCommands) == 0 {
		return
	}
	for model, command := range stored.GeneratorCommands {
		cleanModel := normalizeModelID(model)
		cleanCommand := strings.TrimSpace(command)
		if cleanModel == "" {
			continue
		}
		m.settings.GeneratorCommands[cleanModel] = cleanCommand
		if cleanCommand != "" {
			m.generatorPaths[cleanModel] = cleanCommand
		}
	}
}

func generatorLabel(model string) string {
	switch normalizeModelID(model) {
	case "qwen3tts":
		return "Qwen3-TTS"
	case "gptsovits":
		return "GPT-SoVITS"
	case "kokoro":
		return "Kokoro"
	default:
		return "Piper"
	}
}

func (m *Manager) GeneratorInfos() []GeneratorInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	infos := make([]GeneratorInfo, 0, 4)
	for _, id := range []string{"qwen3tts", "gptsovits", "kokoro", "piper"} {
		command := strings.TrimSpace(m.generatorPaths[id])
		info := GeneratorInfo{
			ID:          id,
			Label:       generatorLabel(id),
			Command:     command,
			Recommended: id == m.settings.PreferredGenerator,
		}
		if err := requireExecutable(command); err != nil {
			info.Available = false
			if command == "" {
				info.Message = "Not configured."
			} else {
				info.Message = err.Error()
			}
		} else {
			info.Available = true
			info.Message = "Ready."
		}
		infos = append(infos, info)
	}
	return infos
}

func (m *Manager) DatasetSummary() DatasetSummary {
	recordings, _ := m.ListRecordings()
	models, _ := m.ListModels()
	feedback, _ := m.ListFeedback()
	summary := DatasetSummary{
		Feedback:          SummarizeFeedback(feedback),
		GeneratedSamples:  countGeneratedSamples(m.generatedDir()),
		GenerationRunning: m.GenerationStatus().State == statusStateRunning,
		TrainingRunning:   m.TrainingStatus().State == statusStateRunning,
	}
	for _, recording := range recordings {
		switch recording.Kind {
		case recordingKindReference:
			summary.ReferenceClips++
		case recordingKindTest:
			summary.TestClips++
		default:
			summary.HotwordClips++
		}
	}
	for _, model := range models {
		if !model.Production && summary.LatestModel == "" {
			summary.LatestModel = model.FileName
		}
		if model.Production && summary.ProductionModel == "" {
			summary.ProductionModel = model.FileName
		}
	}
	for _, model := range models {
		if summary.LatestModel == "" {
			summary.LatestModel = model.FileName
		}
	}
	return summary
}

func (m *Manager) SettingsSnapshot() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := m.settings
	copied.NegativePhrases = append([]string(nil), m.settings.NegativePhrases...)
	copied.GeneratorCommands = map[string]string{}
	for model, command := range m.settings.GeneratorCommands {
		copied.GeneratorCommands[model] = command
	}
	return copied
}

func (m *Manager) SaveSettings(stored Settings) (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applySettingsLocked(stored)
	for _, id := range []string{"qwen3tts", "gptsovits", "kokoro", "piper"} {
		command := strings.TrimSpace(stored.GeneratorCommands[id])
		if command == "" {
			if id == "piper" {
				m.generatorPaths[id] = filepath.Join(m.projectRoot, "scripts", "hotword-generate-piper.sh")
				m.settings.GeneratorCommands[id] = m.generatorPaths[id]
			}
			continue
		}
		m.generatorPaths[id] = command
		m.settings.GeneratorCommands[id] = command
	}
	if m.settings.GeneratorCommands["piper"] == "" {
		m.settings.GeneratorCommands["piper"] = m.generatorPaths["piper"]
	}
	if err := m.persistSettingsLocked(); err != nil {
		return Settings{}, err
	}
	copied := m.settings
	copied.NegativePhrases = append([]string(nil), m.settings.NegativePhrases...)
	copied.GeneratorCommands = map[string]string{}
	for model, command := range m.settings.GeneratorCommands {
		copied.GeneratorCommands[model] = command
	}
	return copied, nil
}

func (m *Manager) trainingScriptPath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.TrimSpace(m.trainingPath)
}

func writeJSONFile(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func decodeJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func isNotFound(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

func statusProgressForIndex(index, total int) int {
	if total <= 0 {
		return 0
	}
	return int(float64(index) / float64(total) * 85)
}

func formatCommandError(err error, lastLine string) string {
	if lastLine != "" {
		return lastLine
	}
	if err != nil {
		return err.Error()
	}
	return "command failed"
}

func requireExecutable(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("generator script not configured")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("expected file, got directory: %s", path)
	}
	return nil
}
