package web

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/hotwordtrain"
)

func TestHotwordTrainPageRequiresAuthAndServesShell(t *testing.T) {
	app := newAuthedTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/hotword-train", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", rr.Code)
	}

	authed := doAuthedHTMLRequest(t, app.Router(), "/hotword-train")
	if authed.Code != http.StatusOK {
		t.Fatalf("authed status = %d, want 200", authed.Code)
	}
	body := authed.Body.String()
	for _, needle := range []string{
		"<title>Hotword Training | Tabura</title>",
		"Train Sloppy",
		`src="./static/hotword-train.js`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("body missing %q", needle)
		}
	}
}

func TestHotwordTrainRecordingCRUDAndAudio(t *testing.T) {
	app := newAuthedTestApp(t)
	root := t.TempDir()
	app.localProjectDir = root
	app.hotwordTrainer = app.hotwordTrainerForTest(root)

	upload := uploadHotwordRecording(t, app, "hotword")
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201 body=%s", upload.Code, upload.Body.String())
	}
	recording := decodeJSONResponse(t, upload)["recording"].(map[string]any)
	recordingID := strFromAny(recording["id"])

	list := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/hotword/train/recordings", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", list.Code)
	}
	recordings, ok := decodeJSONResponse(t, list)["recordings"].([]any)
	if !ok || len(recordings) != 1 {
		t.Fatalf("recordings payload = %#v", decodeJSONResponse(t, list))
	}

	audioReq := httptest.NewRequest(http.MethodGet, "/api/hotword/train/recordings/"+recordingID+"/audio", nil)
	audioReq.AddCookie(&http.Cookie{Name: SessionCookie, Value: testAuthToken})
	audioRR := httptest.NewRecorder()
	app.Router().ServeHTTP(audioRR, audioReq)
	if audioRR.Code != http.StatusOK {
		t.Fatalf("audio status = %d, want 200", audioRR.Code)
	}
	if got := audioRR.Header().Get("Content-Type"); !strings.Contains(got, "audio/wav") {
		t.Fatalf("audio content type = %q, want audio/wav", got)
	}

	deleteRR := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/hotword/train/recordings/"+recordingID, nil)
	if deleteRR.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 body=%s", deleteRR.Code, deleteRR.Body.String())
	}
}

func TestHotwordTrainFeedbackCapture(t *testing.T) {
	app := newAuthedTestApp(t)
	root := t.TempDir()
	app.localProjectDir = root
	app.hotwordTrainer = app.hotwordTrainerForTest(root)

	upload := uploadHotwordRecording(t, app, "test")
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201 body=%s", upload.Code, upload.Body.String())
	}
	recording := decodeJSONResponse(t, upload)["recording"].(map[string]any)
	recordingID := strFromAny(recording["id"])

	feedbackRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/hotword/train/feedback", map[string]any{
		"recording_id": recordingID,
		"outcome":      "missed_trigger",
	})
	if feedbackRR.Code != http.StatusCreated {
		t.Fatalf("feedback status = %d, want 201 body=%s", feedbackRR.Code, feedbackRR.Body.String())
	}
	feedbackPayload := decodeJSONResponse(t, feedbackRR)
	if summary := feedbackPayload["summary"].(map[string]any); int(summary["missed_triggers"].(float64)) != 1 {
		t.Fatalf("summary = %#v, want one missed trigger", summary)
	}

	listRR := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/hotword/train/feedback", nil)
	if listRR.Code != http.StatusOK {
		t.Fatalf("feedback list status = %d, want 200 body=%s", listRR.Code, listRR.Body.String())
	}
	listPayload := decodeJSONResponse(t, listRR)
	feedbackEntries := listPayload["feedback"].([]any)
	if len(feedbackEntries) != 1 {
		t.Fatalf("feedback list = %#v, want one entry", listPayload)
	}
	entry := feedbackEntries[0].(map[string]any)
	if strFromAny(entry["recording_id"]) != recordingID {
		t.Fatalf("feedback recording_id = %q, want %q", strFromAny(entry["recording_id"]), recordingID)
	}
	if strFromAny(entry["outcome"]) != "missed_trigger" {
		t.Fatalf("feedback outcome = %q, want missed_trigger", strFromAny(entry["outcome"]))
	}
}

func TestHotwordTrainJobsStreamStatusAndDeploy(t *testing.T) {
	app := newAuthedTestApp(t)
	root := t.TempDir()
	app.localProjectDir = root
	app.hotwordTrainer = app.hotwordTrainerForTest(root)

	generatorScript := filepath.Join(root, "generate.sh")
	if err := os.WriteFile(generatorScript, []byte(`#!/usr/bin/env bash
set -euo pipefail
OUTPUT_DIR=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-dir) OUTPUT_DIR="$2"; shift 2 ;;
    *) shift ;;
  esac
done
mkdir -p "$OUTPUT_DIR"
cp "$(find "$TABURA_HOTWORD_RECORDINGS_DIR" -maxdepth 1 -type f -name '*.wav' | head -n 1)" "$OUTPUT_DIR/piper-0001.wav"
echo "generated sample"
`), 0o755); err != nil {
		t.Fatalf("write generator script: %v", err)
	}
	trainingScript := filepath.Join(root, "train.sh")
	if err := os.WriteFile(trainingScript, []byte(`#!/usr/bin/env bash
set -euo pipefail
mkdir -p "$TABURA_HOTWORD_OUTPUT_DIR"
printf 'trained-model' >"$TABURA_HOTWORD_OUTPUT_DIR/sloppy-2026-03-23_21-03-09Z.onnx"
printf 'trained-data' >"$TABURA_HOTWORD_OUTPUT_DIR/sloppy-2026-03-23_21-03-09Z.onnx.data"
echo "trained model: $TABURA_HOTWORD_OUTPUT_DIR/sloppy-2026-03-23_21-03-09Z.onnx"
`), 0o755); err != nil {
		t.Fatalf("write training script: %v", err)
	}
	app.hotwordTrainer.SetGeneratorScriptPath("piper", generatorScript)
	app.hotwordTrainer.SetTrainingScriptPath(trainingScript)

	upload := uploadHotwordRecording(t, app, "hotword")
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201", upload.Code)
	}

	generateRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/hotword/train/generate", map[string]any{
		"models":       []string{"piper"},
		"sample_count": 1,
	})
	if generateRR.Code != http.StatusAccepted {
		t.Fatalf("generate status = %d, want 202 body=%s", generateRR.Code, generateRR.Body.String())
	}

	server := httptest.NewServer(app.Router())
	defer server.Close()
	generationEvent := readStatusEvent(t, server.URL+"/api/hotword/train/generate/status")
	if !strings.Contains(generationEvent, `"state":"running"`) && !strings.Contains(generationEvent, `"state":"completed"`) {
		t.Fatalf("generation event = %s, want active status", generationEvent)
	}
	waitForHotwordJob(t, 5*time.Second, func() bool {
		return app.hotwordTrainer.GenerationStatus().State == "completed"
	})

	trainRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/hotword/train/start", map[string]any{})
	if trainRR.Code != http.StatusAccepted {
		t.Fatalf("train status = %d, want 202 body=%s", trainRR.Code, trainRR.Body.String())
	}
	trainingEvent := readStatusEvent(t, server.URL+"/api/hotword/train/status")
	if !strings.Contains(trainingEvent, `"state":"running"`) {
		t.Fatalf("training event = %s, want running status", trainingEvent)
	}
	waitForHotwordJob(t, 5*time.Second, func() bool {
		return app.hotwordTrainer.TrainingStatus().State == "completed"
	})

	modelsRR := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/hotword/train/models", nil)
	if modelsRR.Code != http.StatusOK {
		t.Fatalf("models status = %d, want 200", modelsRR.Code)
	}
	models := decodeJSONResponse(t, modelsRR)["models"].([]any)
	if len(models) == 0 {
		t.Fatalf("models payload = %#v, want non-empty", decodeJSONResponse(t, modelsRR))
	}
	modelPayloadFromList := models[0].(map[string]any)
	modelFileName := strFromAny(modelPayloadFromList["file_name"])
	if modelFileName == "" {
		t.Fatalf("model file name missing from payload: %#v", modelPayloadFromList)
	}

	deployRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/hotword/train/deploy", map[string]any{
		"model": modelFileName,
	})
	if deployRR.Code != http.StatusOK {
		t.Fatalf("deploy status = %d, want 200 body=%s", deployRR.Code, deployRR.Body.String())
	}
	deployPayload := decodeJSONResponse(t, deployRR)
	hotwordStatus, ok := deployPayload["hotword_status"].(map[string]any)
	if !ok {
		t.Fatalf("deploy payload missing hotword_status: %#v", deployPayload)
	}
	modelPayload, ok := hotwordStatus["model"].(map[string]any)
	if !ok {
		t.Fatalf("deploy hotword_status missing model: %#v", hotwordStatus)
	}
	if strFromAny(modelPayload["revision"]) == "" {
		t.Fatalf("deploy hotword revision missing from payload: %#v", modelPayload)
	}
	vendorPath := filepath.Join(root, "internal", "web", "static", "vendor", "openwakeword", "sloppy.onnx")
	data, err := os.ReadFile(vendorPath)
	if err != nil {
		t.Fatalf("read deployed model: %v", err)
	}
	if string(data) != "trained-model" {
		t.Fatalf("deployed model = %q, want %q", string(data), "trained-model")
	}
	vendorDataPath := vendorPath + ".data"
	dataSidecar, err := os.ReadFile(vendorDataPath)
	if err != nil {
		t.Fatalf("read deployed model data: %v", err)
	}
	if string(dataSidecar) != "trained-data" {
		t.Fatalf("deployed model data = %q, want %q", string(dataSidecar), "trained-data")
	}
}

func TestHotwordTrainConfigRoundTrip(t *testing.T) {
	app := newAuthedTestApp(t)
	root := t.TempDir()
	app.localProjectDir = root
	app.hotwordTrainer = app.hotwordTrainerForTest(root)

	getRR := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/hotword/train/config", nil)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET config status = %d, want 200 body=%s", getRR.Code, getRR.Body.String())
	}
	getPayload := decodeJSONResponse(t, getRR)
	config, ok := getPayload["config"].(map[string]any)
	if !ok {
		t.Fatalf("config payload missing: %#v", getPayload)
	}
	settings := config["settings"].(map[string]any)
	if strFromAny(settings["preferred_generator"]) != "qwen3tts" {
		t.Fatalf("preferred_generator = %q, want qwen3tts", strFromAny(settings["preferred_generator"]))
	}

	putRR := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/hotword/train/config", map[string]any{
		"preferred_generator": "piper",
		"sample_count":        3000,
		"auto_deploy":         false,
		"negative_phrases":    []string{"copy", "slowly"},
		"generator_commands": map[string]any{
			"qwen3tts": "/tmp/qwen3tts-hotword",
			"piper":    "/tmp/piper-hotword",
		},
	})
	if putRR.Code != http.StatusOK {
		t.Fatalf("PUT config status = %d, want 200 body=%s", putRR.Code, putRR.Body.String())
	}
	putPayload := decodeJSONResponse(t, putRR)
	putConfig := putPayload["config"].(map[string]any)
	putSettings := putConfig["settings"].(map[string]any)
	if strFromAny(putSettings["preferred_generator"]) != "piper" {
		t.Fatalf("updated preferred_generator = %q, want piper", strFromAny(putSettings["preferred_generator"]))
	}
	if int(putSettings["sample_count"].(float64)) != 3000 {
		t.Fatalf("updated sample_count = %v, want 3000", putSettings["sample_count"])
	}
}

func TestHotwordTrainPipelineStart(t *testing.T) {
	app := newAuthedTestApp(t)
	root := t.TempDir()
	app.localProjectDir = root
	app.hotwordTrainer = app.hotwordTrainerForTest(root)

	generatorScript := filepath.Join(root, "generate.sh")
	if err := os.WriteFile(generatorScript, []byte(`#!/usr/bin/env bash
set -euo pipefail
OUTPUT_DIR=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-dir) OUTPUT_DIR="$2"; shift 2 ;;
    *) shift ;;
  esac
done
mkdir -p "$OUTPUT_DIR"
printf 'RIFF....WAVEfmt ' >"$OUTPUT_DIR/piper-0001.wav"
echo "generated sample"
`), 0o755); err != nil {
		t.Fatalf("write generator script: %v", err)
	}
	trainingScript := filepath.Join(root, "train.sh")
	if err := os.WriteFile(trainingScript, []byte(`#!/usr/bin/env bash
set -euo pipefail
mkdir -p "$TABURA_HOTWORD_OUTPUT_DIR"
printf 'trained-model' >"$TABURA_HOTWORD_OUTPUT_DIR/sloppy-2026-03-23_21-03-09Z.onnx"
printf 'trained-data' >"$TABURA_HOTWORD_OUTPUT_DIR/sloppy-2026-03-23_21-03-09Z.onnx.data"
echo "trained model: $TABURA_HOTWORD_OUTPUT_DIR/sloppy-2026-03-23_21-03-09Z.onnx"
`), 0o755); err != nil {
		t.Fatalf("write training script: %v", err)
	}
	app.hotwordTrainer.SetGeneratorScriptPath("piper", generatorScript)
	app.hotwordTrainer.SetTrainingScriptPath(trainingScript)

	upload := uploadHotwordRecording(t, app, "hotword")
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201", upload.Code)
	}

	pipelineRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/hotword/train/pipeline", map[string]any{
		"models": []string{"piper"},
	})
	if pipelineRR.Code != http.StatusAccepted {
		t.Fatalf("pipeline status = %d, want 202 body=%s", pipelineRR.Code, pipelineRR.Body.String())
	}

	waitForHotwordJob(t, 5*time.Second, func() bool {
		status := app.hotwordTrainer.TrainingStatus()
		return status.State == "completed" || status.State == "failed"
	})
	status := app.hotwordTrainer.TrainingStatus()
	if status.State != "completed" {
		t.Fatalf("pipeline state = %q, want completed (message=%q)", status.State, status.Message)
	}
	if !strings.Contains(status.Message, "deployed") {
		t.Fatalf("pipeline message = %q, want deployed", status.Message)
	}
}

func (a *App) hotwordTrainerForTest(projectRoot string) *hotwordtrain.Manager {
	manager := hotwordtrain.New(a.dataDir, projectRoot)
	return manager
}

func uploadHotwordRecording(t *testing.T, app *App, kind string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("kind", kind); err != nil {
		t.Fatalf("WriteField(kind): %v", err)
	}
	part, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatalf("CreateFormFile(file): %v", err)
	}
	if _, err := part.Write(testHotwordWAV()); err != nil {
		t.Fatalf("write wav: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close(): %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/hotword/train/recordings", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testAuthToken})
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)
	return rr
}

func testHotwordWAV() []byte {
	samples := []int16{0, 1000, -1000, 500, -500, 0}
	dataSize := len(samples) * 2
	buf := bytes.NewBuffer(make([]byte, 0, 44+dataSize))
	buf.WriteString("RIFF")
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint32(16000))
	_ = binary.Write(buf, binary.LittleEndian, uint32(32000))
	_ = binary.Write(buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(buf, binary.LittleEndian, uint32(dataSize))
	for _, sample := range samples {
		_ = binary.Write(buf, binary.LittleEndian, sample)
	}
	return buf.Bytes()
}

func readStatusEvent(t *testing.T, path string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("NewRequest(%s): %v", path, err)
	}
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testAuthToken})
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("ReadString: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		}
	}
}

func waitForHotwordJob(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for hotword job")
}
