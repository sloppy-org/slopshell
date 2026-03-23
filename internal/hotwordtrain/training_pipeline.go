package hotwordtrain

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	trainerOutputDirName = "trainer-output"
	trainerConfigDirName = "trainer-config"
)

type trainingRun struct {
	ConfigPath string
	OutputDir  string
	ModelDir   string
	ModelName  string
}

type stagedDatasetSummary struct {
	PositiveTrain int
	PositiveTest  int
	NegativeTrain int
	NegativeTest  int
}

func (m *Manager) baseConfigPath() string {
	projectPath := filepath.Join(m.projectRoot, "scripts", "hotword-config.yaml")
	if _, err := os.Stat(projectPath); err == nil {
		return projectPath
	}
	_, file, _, ok := runtime.Caller(0)
	if ok {
		fallback := filepath.Join(filepath.Dir(file), "..", "..", "scripts", "hotword-config.yaml")
		if _, err := os.Stat(fallback); err == nil {
			return fallback
		}
	}
	return filepath.Join("scripts", "hotword-config.yaml")
}

func (m *Manager) trainerOutputDir() string {
	return filepath.Join(m.dataDir, "hotword-train", trainerOutputDirName)
}

func (m *Manager) trainerConfigDir() string {
	return filepath.Join(m.dataDir, "hotword-train", trainerConfigDirName)
}

func (m *Manager) prepareTrainingRun(req TrainRequest) (trainingRun, error) {
	basePath := strings.TrimSpace(req.ConfigPath)
	if basePath == "" {
		basePath = m.baseConfigPath()
	}
	data, err := os.ReadFile(basePath)
	if err != nil {
		return trainingRun{}, err
	}
	sampleCount := req.SampleCount
	if sampleCount <= 0 {
		sampleCount = defaultSettings().SampleCount
	}
	negativePhrases := normalizeNegativePhrases(req.NegativePhrases)
	runStamp := timeSafeStamp()
	outputDir := filepath.Join(m.trainerOutputDir(), runStamp)
	if err := m.ensureDir(outputDir); err != nil {
		return trainingRun{}, err
	}
	if err := m.ensureDir(m.trainerConfigDir()); err != nil {
		return trainingRun{}, err
	}
	modelName := configScalar(string(data), "model_name")
	if modelName == "" {
		modelName = defaultModelName
	}
	rendered := patchTrainingConfig(string(data), outputDir, sampleCount, negativePhrases)
	configPath := filepath.Join(m.trainerConfigDir(), fmt.Sprintf("%s-%s.yaml", modelName, runStamp))
	if err := os.WriteFile(configPath, []byte(rendered), 0o644); err != nil {
		return trainingRun{}, err
	}
	return trainingRun{
		ConfigPath: configPath,
		OutputDir:  outputDir,
		ModelDir:   filepath.Join(outputDir, modelName),
		ModelName:  modelName,
	}, nil
}

func configScalar(content, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		return strings.Trim(value, `"'`)
	}
	return ""
}

func patchTrainingConfig(content, outputDir string, sampleCount int, negativePhrases []string) string {
	lines := strings.Split(content, "\n")
	replacedNegatives := false
	out := make([]string, 0, len(lines)+len(negativePhrases))
	skippingNegativeItems := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "custom_negative_phrases:"):
			out = append(out, "custom_negative_phrases:")
			for _, phrase := range negativePhrases {
				out = append(out, fmt.Sprintf("  - %q", phrase))
			}
			replacedNegatives = true
			skippingNegativeItems = true
		case skippingNegativeItems && (strings.HasPrefix(strings.TrimLeft(line, " "), "- ") || trimmed == ""):
			continue
		case skippingNegativeItems:
			skippingNegativeItems = false
			out = appendPatchedConfigLine(out, line, outputDir, sampleCount)
		default:
			out = appendPatchedConfigLine(out, line, outputDir, sampleCount)
		}
	}
	if !replacedNegatives {
		out = append(out, "custom_negative_phrases:")
		for _, phrase := range negativePhrases {
			out = append(out, fmt.Sprintf("  - %q", phrase))
		}
	}
	return strings.Join(out, "\n")
}

func appendPatchedConfigLine(out []string, line, outputDir string, sampleCount int) []string {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "n_samples:"):
		return append(out, fmt.Sprintf("n_samples: %d", sampleCount))
	case strings.HasPrefix(trimmed, "n_samples_val:"):
		return append(out, fmt.Sprintf("n_samples_val: %d", trainingValidationSamples(sampleCount)))
	case strings.HasPrefix(trimmed, "output_dir:"):
		return append(out, fmt.Sprintf("output_dir: %q", outputDir))
	default:
		return append(out, line)
	}
}

func trainingValidationSamples(sampleCount int) int {
	if sampleCount <= 0 {
		return 1000
	}
	target := sampleCount / 5
	if target < 250 {
		target = 250
	}
	if target > 2000 {
		target = 2000
	}
	return target
}

func stagedPositiveSources(recordings []Recording, feedbackByRecording map[string][]Feedback, generatedDirs []string) []stagedClip {
	out := make([]stagedClip, 0, len(recordings))
	for _, recording := range recordings {
		switch recording.Kind {
		case recordingKindHotword:
			out = append(out, stagedClip{ID: recording.ID, Path: recording.FileName, SourceKind: "recording"})
		case recordingKindTest:
			for _, entry := range feedbackByRecording[recording.ID] {
				if entry.Outcome == feedbackOutcomeMissedTrigger {
					out = append(out, stagedClip{ID: recording.ID + "-" + entry.ID, Path: recording.FileName, SourceKind: "missed_trigger"})
				}
			}
		}
	}
	for _, root := range generatedDirs {
		matches, _ := filepath.Glob(filepath.Join(root, "*.wav"))
		sort.Strings(matches)
		for _, match := range matches {
			out = append(out, stagedClip{ID: filepath.Base(root) + "-" + filepath.Base(match), Path: match, SourceKind: "generated"})
		}
	}
	return out
}

func stagedNegativeSources(recordings []Recording, feedbackByRecording map[string][]Feedback) []stagedClip {
	out := make([]stagedClip, 0, len(recordings))
	for _, recording := range recordings {
		if recording.Kind != recordingKindTest {
			continue
		}
		for _, entry := range feedbackByRecording[recording.ID] {
			if entry.Outcome == feedbackOutcomeFalseTrigger {
				out = append(out, stagedClip{ID: recording.ID + "-" + entry.ID, Path: recording.FileName, SourceKind: "false_trigger"})
			}
		}
	}
	return out
}

type stagedClip struct {
	ID         string
	Path       string
	SourceKind string
}

func (m *Manager) stageTrainingDataset(run trainingRun, generatedDirs []string) (stagedDatasetSummary, error) {
	recordings, err := m.ListRecordings()
	if err != nil {
		return stagedDatasetSummary{}, err
	}
	feedback, err := m.ListFeedback()
	if err != nil {
		return stagedDatasetSummary{}, err
	}
	if err := m.ensureDir(run.ModelDir); err != nil {
		return stagedDatasetSummary{}, err
	}
	positiveTrainDir := filepath.Join(run.ModelDir, "positive_train")
	positiveTestDir := filepath.Join(run.ModelDir, "positive_test")
	negativeTrainDir := filepath.Join(run.ModelDir, "adversarial_negative_train")
	negativeTestDir := filepath.Join(run.ModelDir, "adversarial_negative_test")
	for _, dir := range []string{positiveTrainDir, positiveTestDir, negativeTrainDir, negativeTestDir} {
		if err := m.ensureDir(dir); err != nil {
			return stagedDatasetSummary{}, err
		}
	}
	feedbackByRecording := make(map[string][]Feedback)
	for _, entry := range feedback {
		feedbackByRecording[entry.RecordingID] = append(feedbackByRecording[entry.RecordingID], entry)
	}
	positives := stagedPositiveSources(recordings, feedbackByRecording, generatedDirs)
	negatives := stagedNegativeSources(recordings, feedbackByRecording)
	summary := stagedDatasetSummary{}
	for _, clip := range positives {
		sourcePath := clip.Path
		if !filepath.IsAbs(sourcePath) {
			sourcePath = filepath.Join(m.recordingsDir(), sourcePath)
		}
		targetDir := chooseTrainOrTestDir(clip.ID, positiveTrainDir, positiveTestDir)
		if err := copyClip(sourcePath, filepath.Join(targetDir, stagedClipFileName(clip))); err != nil {
			return stagedDatasetSummary{}, err
		}
		if targetDir == positiveTrainDir {
			summary.PositiveTrain++
		} else {
			summary.PositiveTest++
		}
	}
	for _, clip := range negatives {
		sourcePath := clip.Path
		if !filepath.IsAbs(sourcePath) {
			sourcePath = filepath.Join(m.recordingsDir(), sourcePath)
		}
		targetDir := chooseTrainOrTestDir(clip.ID, negativeTrainDir, negativeTestDir)
		if err := copyClip(sourcePath, filepath.Join(targetDir, stagedClipFileName(clip))); err != nil {
			return stagedDatasetSummary{}, err
		}
		if targetDir == negativeTrainDir {
			summary.NegativeTrain++
		} else {
			summary.NegativeTest++
		}
	}
	return summary, nil
}

func chooseTrainOrTestDir(seed, trainDir, testDir string) string {
	sum := sha1.Sum([]byte(seed))
	if int(sum[0])%5 == 0 {
		return testDir
	}
	return trainDir
}

func stagedClipFileName(clip stagedClip) string {
	base := filepath.Base(clip.Path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return fmt.Sprintf("%s-%s%s", clip.SourceKind, name, ext)
}

func copyClip(sourcePath, targetPath string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, 0o644)
}

func countGeneratedSamples(root string) int {
	total := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".wav") {
			total++
		}
		return nil
	})
	return total
}
