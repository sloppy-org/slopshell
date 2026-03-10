package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

const testScanPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3xwAAAAASUVORK5CYII="

type fakeScanExtractor struct {
	result scanExtractResult
	err    error
}

func (f fakeScanExtractor) ExtractScan(_ context.Context, _ scanExtractRequest) (scanExtractResult, error) {
	return f.result, f.err
}

func firstNonHubProject(t *testing.T, app *App) store.Project {
	t.Helper()
	projects, err := app.store.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects() error: %v", err)
	}
	for _, project := range projects {
		if !strings.EqualFold(project.Kind, "hub") {
			return project
		}
	}
	t.Fatal("expected non-hub project")
	return store.Project{}
}

func TestScanUploadCreatesLinkedArtifactAndSummary(t *testing.T) {
	app := newAuthedTestApp(t)
	app.scanExtractor = fakeScanExtractor{
		result: scanExtractResult{
			Summary: "One handwritten note detected.",
			Annotations: []scanMappedAnnotation{
				{Content: "check null case", AnchorText: "Line two", Line: 2, Confidence: 0.9},
			},
		},
	}

	project := firstNonHubProject(t, app)
	sourceArtifact, err := app.store.CreateArtifact(
		store.ArtifactKindMarkdown,
		nil,
		nil,
		stringPtr("notes.md"),
		stringPtr(`{"text":"Line one\nLine two\nLine three"}`),
	)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	item, err := app.store.CreateItem("Review packet", store.ItemOptions{
		State:      store.ItemStateInbox,
		ArtifactID: &sourceArtifact.ID,
		ProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	imageBytes, err := base64.StdEncoding.DecodeString(testScanPNGBase64)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	rr := authedMultipartRequestForTest(t, app.Router(), "/api/scan/upload", func(writer *multipart.Writer) {
		if err := writer.WriteField("project_id", project.ID); err != nil {
			t.Fatalf("WriteField(project_id): %v", err)
		}
		if err := writer.WriteField("item_id", strconv.FormatInt(item.ID, 10)); err != nil {
			t.Fatalf("WriteField(item_id): %v", err)
		}
		if err := writer.WriteField("artifact_id", strconv.FormatInt(sourceArtifact.ID, 10)); err != nil {
			t.Fatalf("WriteField(artifact_id): %v", err)
		}
		part, err := writer.CreateFormFile("file", "annotated.png")
		if err != nil {
			t.Fatalf("CreateFormFile(file): %v", err)
		}
		if _, err := part.Write(imageBytes); err != nil {
			t.Fatalf("write image: %v", err)
		}
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("scan upload status=%d body=%s", rr.Code, rr.Body.String())
	}

	data := decodeJSONDataResponse(t, rr)
	if got := strFromAny(data["project_id"]); got != project.ID {
		t.Fatalf("project_id = %q, want %q", got, project.ID)
	}
	if got := int64FromAny(data["item_id"]); got != item.ID {
		t.Fatalf("item_id = %d, want %d", got, item.ID)
	}
	if got := int64FromAny(data["artifact_id"]); got != sourceArtifact.ID {
		t.Fatalf("artifact_id = %d, want %d", got, sourceArtifact.ID)
	}
	summaryPath := strFromAny(data["summary_path"])
	if summaryPath == "" {
		t.Fatal("expected summary_path")
	}
	summaryContent, err := os.ReadFile(filepath.Join(project.RootPath, filepath.FromSlash(summaryPath)))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if !strings.Contains(string(summaryContent), "check null case") {
		t.Fatalf("summary missing extracted note: %s", string(summaryContent))
	}
	scanArtifactPayload, ok := data["scan_artifact"].(map[string]any)
	if !ok {
		t.Fatalf("scan_artifact payload = %#v", data["scan_artifact"])
	}
	scanArtifactID := int64FromAny(scanArtifactPayload["id"])
	if scanArtifactID <= 0 {
		t.Fatalf("scan_artifact id = %d", scanArtifactID)
	}
	scanArtifact, err := app.store.GetArtifact(scanArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact(scan) error: %v", err)
	}
	if scanArtifact.Kind != store.ArtifactKindImage {
		t.Fatalf("scan artifact kind = %q, want %q", scanArtifact.Kind, store.ArtifactKindImage)
	}
	var meta scanArtifactMeta
	if err := json.Unmarshal([]byte(strings.TrimSpace(optionalStringValue(scanArtifact.MetaJSON))), &meta); err != nil {
		t.Fatalf("decode scan artifact meta: %v", err)
	}
	if meta.ItemID != item.ID || meta.SourceArtifactID != sourceArtifact.ID {
		t.Fatalf("scan meta ids = %+v", meta)
	}
	if len(meta.Extraction) != 1 || meta.Extraction[0].Content != "check null case" {
		t.Fatalf("scan meta extraction = %+v", meta.Extraction)
	}
	linked, err := app.store.ListItemArtifacts(item.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts() error: %v", err)
	}
	if len(linked) < 2 {
		t.Fatalf("expected source + scan artifacts, got %+v", linked)
	}
}

func TestScanConfirmCreatesAnnotationArtifactAndUpdatesScanMeta(t *testing.T) {
	app := newAuthedTestApp(t)
	app.scanExtractor = fakeScanExtractor{
		result: scanExtractResult{
			Annotations: []scanMappedAnnotation{
				{Content: "check null case", AnchorText: "Line two", Line: 2, Confidence: 0.8},
			},
		},
	}

	project := firstNonHubProject(t, app)
	sourceArtifact, err := app.store.CreateArtifact(
		store.ArtifactKindMarkdown,
		nil,
		nil,
		stringPtr("notes.md"),
		stringPtr(`{"text":"Line one\nLine two\nLine three"}`),
	)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	item, err := app.store.CreateItem("Review packet", store.ItemOptions{
		State:      store.ItemStateInbox,
		ArtifactID: &sourceArtifact.ID,
		ProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	imageBytes, err := base64.StdEncoding.DecodeString(testScanPNGBase64)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	upload := authedMultipartRequestForTest(t, app.Router(), "/api/scan/upload", func(writer *multipart.Writer) {
		if err := writer.WriteField("project_id", project.ID); err != nil {
			t.Fatalf("WriteField(project_id): %v", err)
		}
		if err := writer.WriteField("item_id", strconv.FormatInt(item.ID, 10)); err != nil {
			t.Fatalf("WriteField(item_id): %v", err)
		}
		if err := writer.WriteField("artifact_id", strconv.FormatInt(sourceArtifact.ID, 10)); err != nil {
			t.Fatalf("WriteField(artifact_id): %v", err)
		}
		part, err := writer.CreateFormFile("file", "annotated.png")
		if err != nil {
			t.Fatalf("CreateFormFile(file): %v", err)
		}
		if _, err := part.Write(imageBytes); err != nil {
			t.Fatalf("write image: %v", err)
		}
	})
	if upload.Code != http.StatusCreated {
		t.Fatalf("scan upload status=%d body=%s", upload.Code, upload.Body.String())
	}
	uploadData := decodeJSONDataResponse(t, upload)
	scanArtifact := uploadData["scan_artifact"].(map[string]any)
	scanArtifactID := int64FromAny(scanArtifact["id"])

	confirm := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/scan/confirm", map[string]any{
		"project_id":       project.ID,
		"item_id":          item.ID,
		"artifact_id":      sourceArtifact.ID,
		"scan_artifact_id": scanArtifactID,
		"annotations": []map[string]any{
			{
				"content":     "check nil case",
				"anchor_text": "Line two",
				"line":        2,
				"confidence":  0.95,
				"bounds": map[string]any{
					"x":      0.02,
					"y":      0.18,
					"width":  0.96,
					"height": 0.05,
				},
			},
		},
	})
	if confirm.Code != http.StatusCreated {
		t.Fatalf("scan confirm status=%d body=%s", confirm.Code, confirm.Body.String())
	}
	data := decodeJSONDataResponse(t, confirm)
	reviewArtifactPayload, ok := data["review_artifact"].(map[string]any)
	if !ok {
		t.Fatalf("review_artifact payload = %#v", data["review_artifact"])
	}
	reviewArtifactID := int64FromAny(reviewArtifactPayload["id"])
	reviewArtifact, err := app.store.GetArtifact(reviewArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact(review) error: %v", err)
	}
	if reviewArtifact.Kind != store.ArtifactKindAnnotation {
		t.Fatalf("review artifact kind = %q, want %q", reviewArtifact.Kind, store.ArtifactKindAnnotation)
	}
	summaryPath := strFromAny(data["summary_path"])
	if summaryPath == "" {
		t.Fatal("expected confirm summary_path")
	}
	content, err := os.ReadFile(filepath.Join(project.RootPath, filepath.FromSlash(summaryPath)))
	if err != nil {
		t.Fatalf("read confirm summary: %v", err)
	}
	if !strings.Contains(string(content), "check nil case") {
		t.Fatalf("confirm summary missing reviewed text: %s", string(content))
	}

	updatedScanArtifact, err := app.store.GetArtifact(scanArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact(updated scan) error: %v", err)
	}
	var meta scanArtifactMeta
	if err := json.Unmarshal([]byte(strings.TrimSpace(optionalStringValue(updatedScanArtifact.MetaJSON))), &meta); err != nil {
		t.Fatalf("decode updated scan meta: %v", err)
	}
	if meta.Status != "confirmed" || meta.ReviewArtifactID != reviewArtifactID {
		t.Fatalf("updated scan meta = %+v", meta)
	}
	if len(meta.Reviewed) != 1 || meta.Reviewed[0].Content != "check nil case" {
		t.Fatalf("updated scan reviewed = %+v", meta.Reviewed)
	}
	linked, err := app.store.ListItemArtifacts(item.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts() error: %v", err)
	}
	if len(linked) < 3 {
		t.Fatalf("expected source + scan + review artifacts, got %+v", linked)
	}
}

func TestScanSummariesAndPromptPreserveParagraphMarkers(t *testing.T) {
	project := store.Project{ID: "demo"}
	item := &store.Item{Title: "Review email"}
	artifact := &store.Artifact{Title: stringPtr("thread.eml")}

	annotations := []scanMappedAnnotation{
		{Content: "reply here", AnchorText: "Second paragraph", Paragraph: 2, Confidence: 0.88},
	}

	uploadSummary := buildScanUploadSummary(project, item, artifact, ".tabura/artifacts/scans/demo.png", scanExtractResult{
		Summary:     "One margin note detected.",
		Annotations: annotations,
	})
	if !strings.Contains(uploadSummary, "paragraph 2") {
		t.Fatalf("upload summary missing paragraph marker:\n%s", uploadSummary)
	}

	confirmSummary := buildScanConfirmSummary(project, item, artifact, annotations)
	if !strings.Contains(confirmSummary, "paragraph 2") {
		t.Fatalf("confirm summary missing paragraph marker:\n%s", confirmSummary)
	}

	prompt := buildScanExtractionUserPrompt(scanExtractRequest{
		Filename:       "thread-scan.png",
		MIMEType:       "image/png",
		Item:           item,
		SourceArtifact: artifact,
		SourceText:     "P01 First paragraph.\n\nP02 Second paragraph.",
	})
	for _, snippet := range []string{
		"L001 for lines",
		"P01 for paragraphs",
		"Source artifact title: thread.eml",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("scan prompt missing %q:\n%s", snippet, prompt)
		}
	}
}
