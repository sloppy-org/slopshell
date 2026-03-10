package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

const (
	scanMaxImageBytes          = 10 * 1024 * 1024
	scanMultipartOverheadBytes = 1 * 1024 * 1024
	scanMultipartFieldLimit    = 32 * 1024
	scanExtractorTimeout       = 45 * time.Second
)

var (
	errMissingScanFile      = errors.New("missing scan image")
	errDuplicateScanFile    = errors.New("multiple scan images are not supported")
	errInvalidScanImage     = errors.New("scan upload must be a supported image")
	errScanPayloadTooLarge  = errors.New("scan payload exceeds max size")
	errScanExtractorOffline = errors.New("scan extraction is not configured")
)

type scanExtractProvider interface {
	ExtractScan(ctx context.Context, req scanExtractRequest) (scanExtractResult, error)
}

type scanExtractRequest struct {
	Image            []byte
	MIMEType         string
	Filename         string
	Project          store.Project
	Item             *store.Item
	SourceArtifact   *store.Artifact
	SourceText       string
	SourceMetaPretty string
}

type scanExtractResult struct {
	Summary     string                 `json:"summary,omitempty"`
	Annotations []scanMappedAnnotation `json:"annotations"`
}

type scanMappedAnnotation struct {
	Content    string          `json:"content"`
	AnchorText string          `json:"anchor_text,omitempty"`
	Line       int             `json:"line,omitempty"`
	Paragraph  int             `json:"paragraph,omitempty"`
	Page       int             `json:"page,omitempty"`
	Bounds     *scanNormBounds `json:"bounds,omitempty"`
	Confidence float64         `json:"confidence,omitempty"`
}

type scanNormBounds struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type scanUploadPayload struct {
	ProjectID  string
	ItemID     int64
	ArtifactID int64
	Filename   string
	MIMEType   string
	Image      []byte
}

type scanArtifactMeta struct {
	Status               string                 `json:"status"`
	ProjectID            string                 `json:"project_id,omitempty"`
	ItemID               int64                  `json:"item_id,omitempty"`
	SourceArtifactID     int64                  `json:"source_artifact_id,omitempty"`
	Summary              string                 `json:"summary,omitempty"`
	SummaryPath          string                 `json:"summary_path,omitempty"`
	ScanPath             string                 `json:"scan_path,omitempty"`
	Extraction           []scanMappedAnnotation `json:"extraction,omitempty"`
	Reviewed             []scanMappedAnnotation `json:"reviewed,omitempty"`
	ReviewArtifactID     int64                  `json:"review_artifact_id,omitempty"`
	ConfirmedSummaryPath string                 `json:"confirmed_summary_path,omitempty"`
}

type scanConfirmRequest struct {
	ProjectID      string                 `json:"project_id"`
	ItemID         int64                  `json:"item_id"`
	ArtifactID     int64                  `json:"artifact_id"`
	ScanArtifactID int64                  `json:"scan_artifact_id"`
	Annotations    []scanMappedAnnotation `json:"annotations"`
}

type scanLLMResponse struct {
	Annotations []scanMappedAnnotation `json:"annotations"`
	Summary     string                 `json:"summary"`
}

func (a *App) handleScanUpload(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	payload, err := readScanUploadMultipart(w, r)
	if err != nil {
		switch {
		case errors.Is(err, errScanPayloadTooLarge):
			writeAPIError(w, http.StatusRequestEntityTooLarge, err.Error())
		case errors.Is(err, errMissingScanFile), errors.Is(err, errDuplicateScanFile), errors.Is(err, errInvalidMultipartPayload), errors.Is(err, errInvalidScanImage):
			writeAPIError(w, http.StatusBadRequest, err.Error())
		default:
			writeAPIError(w, http.StatusBadRequest, "failed to read scan payload")
		}
		return
	}

	project, item, artifact, err := a.resolveScanContext(payload.ProjectID, payload.ItemID, payload.ArtifactID)
	if err != nil {
		switch {
		case isNoRows(err):
			writeAPIError(w, http.StatusNotFound, "scan source not found")
		default:
			writeAPIError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	scanDir := filepath.Join(project.RootPath, ".tabura", "artifacts", "scans")
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	baseName := sanitizeReviewFilename(strings.TrimSuffix(payload.Filename, filepath.Ext(payload.Filename)))
	if baseName == "" || baseName == "review" {
		baseName = "scan"
	}
	ext := extensionForScanMime(payload.MIMEType, payload.Filename)
	imageName := fmt.Sprintf("%s-%s%s", stamp, baseName, ext)
	imagePath := filepath.Join(scanDir, imageName)
	if err := os.WriteFile(imagePath, payload.Image, 0o644); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	relImagePath, err := filepath.Rel(project.RootPath, imagePath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	relImagePath = filepath.ToSlash(relImagePath)

	sourceText, sourceMetaPretty := "", ""
	if artifact != nil {
		sourceText = strings.TrimSpace(printableArtifactContent(*artifact, optionalStringValue(artifact.MetaJSON)))
		_, sourceMetaPretty = printableArtifactMeta(artifact.MetaJSON)
	}
	result, err := a.extractScanAnnotations(r.Context(), scanExtractRequest{
		Image:            payload.Image,
		MIMEType:         payload.MIMEType,
		Filename:         payload.Filename,
		Project:          project,
		Item:             item,
		SourceArtifact:   artifact,
		SourceText:       sourceText,
		SourceMetaPretty: sourceMetaPretty,
	})
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errScanExtractorOffline) {
			status = http.StatusServiceUnavailable
		}
		writeAPIError(w, status, err.Error())
		return
	}
	summaryPath, err := writeScanSummary(project.RootPath, scanDir, stamp, baseName, buildScanUploadSummary(project, item, artifact, relImagePath, result))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	meta := scanArtifactMeta{
		Status:      "uploaded",
		ProjectID:   project.ID,
		Summary:     strings.TrimSpace(result.Summary),
		SummaryPath: summaryPath,
		ScanPath:    relImagePath,
		Extraction:  result.Annotations,
	}
	if item != nil {
		meta.ItemID = item.ID
	}
	if artifact != nil {
		meta.SourceArtifactID = artifact.ID
	}
	metaJSON, err := marshalScanMeta(meta)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	title := firstNonEmptyScan(
		fmt.Sprintf("Scanned annotations: %s", strings.TrimSpace(optionalStringValue(artifactTitlePtr(artifact, item)))),
		"Scanned annotations",
	)
	scanArtifact, err := a.store.CreateArtifact(store.ArtifactKindImage, scanStringPtr(relImagePath), nil, scanStringPtr(title), &metaJSON)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	if item != nil {
		if err := a.store.LinkItemArtifact(item.ID, scanArtifact.ID, "related"); err != nil {
			writeDomainStoreError(w, err)
			return
		}
	}

	writeAPIData(w, http.StatusCreated, map[string]any{
		"project_id":    project.ID,
		"item_id":       itemIDValue(item),
		"artifact_id":   artifactIDValue(artifact),
		"scan_artifact": scanArtifact,
		"summary_path":  summaryPath,
		"annotations":   result.Annotations,
	})
}

func (a *App) handleScanConfirm(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req scanConfirmRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.ScanArtifactID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "scan_artifact_id is required")
		return
	}
	project, item, artifact, err := a.resolveScanContext(req.ProjectID, req.ItemID, req.ArtifactID)
	if err != nil {
		switch {
		case isNoRows(err):
			writeAPIError(w, http.StatusNotFound, "scan source not found")
		default:
			writeAPIError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	scanArtifact, err := a.store.GetArtifact(req.ScanArtifactID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	meta, _ := parseScanArtifactMeta(scanArtifact.MetaJSON)
	if item == nil && meta.ItemID > 0 {
		if loadedItem, itemErr := a.store.GetItem(meta.ItemID); itemErr == nil {
			item = &loadedItem
		}
	}
	if artifact == nil && meta.SourceArtifactID > 0 {
		if loadedArtifact, artifactErr := a.store.GetArtifact(meta.SourceArtifactID); artifactErr == nil {
			artifact = &loadedArtifact
		}
	}
	annotations := sanitizeScanAnnotations(req.Annotations)
	if len(annotations) == 0 {
		writeAPIError(w, http.StatusBadRequest, "annotations are required")
		return
	}
	scanDir := filepath.Join(project.RootPath, ".tabura", "artifacts", "scans")
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	baseName := sanitizeReviewFilename(strings.TrimSuffix(optionalStringValue(scanArtifact.Title), filepath.Ext(optionalStringValue(scanArtifact.Title))))
	if baseName == "" || baseName == "review" {
		baseName = "scan-review"
	}
	reviewSummaryPath, err := writeScanSummary(project.RootPath, scanDir, stamp, baseName+"-confirmed", buildScanConfirmSummary(project, item, artifact, annotations))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	reviewMeta := scanArtifactMeta{
		Status:           "confirmed",
		ProjectID:        project.ID,
		ItemID:           itemIDValue(item),
		SourceArtifactID: artifactIDValue(artifact),
		Summary:          strings.TrimSpace(meta.Summary),
		Extraction:       meta.Extraction,
		Reviewed:         annotations,
		ScanPath:         meta.ScanPath,
		SummaryPath:      meta.SummaryPath,
	}
	reviewMetaJSON, err := marshalScanMeta(reviewMeta)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	reviewTitle := firstNonEmptyScan(
		fmt.Sprintf("Reviewed annotations: %s", strings.TrimSpace(optionalStringValue(artifactTitlePtr(artifact, item)))),
		"Reviewed annotations",
	)
	reviewArtifact, err := a.store.CreateArtifact(store.ArtifactKindAnnotation, scanStringPtr(reviewSummaryPath), nil, scanStringPtr(reviewTitle), &reviewMetaJSON)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	if item != nil {
		if err := a.store.LinkItemArtifact(item.ID, reviewArtifact.ID, "related"); err != nil {
			writeDomainStoreError(w, err)
			return
		}
	}

	meta.Status = "confirmed"
	meta.Reviewed = annotations
	meta.ReviewArtifactID = reviewArtifact.ID
	meta.ConfirmedSummaryPath = reviewSummaryPath
	metaJSON, err := marshalScanMeta(meta)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.store.UpdateArtifact(scanArtifact.ID, store.ArtifactUpdate{MetaJSON: &metaJSON}); err != nil {
		writeDomainStoreError(w, err)
		return
	}

	writeAPIData(w, http.StatusCreated, map[string]any{
		"project_id":       project.ID,
		"item_id":          itemIDValue(item),
		"artifact_id":      artifactIDValue(artifact),
		"scan_artifact_id": scanArtifact.ID,
		"review_artifact":  reviewArtifact,
		"summary_path":     reviewSummaryPath,
		"annotations":      annotations,
	})
}

func (a *App) resolveScanContext(projectID string, itemID, artifactID int64) (store.Project, *store.Item, *store.Artifact, error) {
	project, err := a.resolveProjectByIDOrActive(projectID)
	if err != nil {
		return store.Project{}, nil, nil, err
	}
	var item *store.Item
	if itemID > 0 {
		loaded, err := a.store.GetItem(itemID)
		if err != nil {
			return store.Project{}, nil, nil, err
		}
		item = &loaded
	}
	if artifactID <= 0 && item != nil && item.ArtifactID != nil {
		artifactID = *item.ArtifactID
	}
	var artifact *store.Artifact
	if artifactID > 0 {
		loaded, err := a.store.GetArtifact(artifactID)
		if err != nil {
			return store.Project{}, nil, nil, err
		}
		artifact = &loaded
	}
	return project, item, artifact, nil
}

func readScanUploadMultipart(w http.ResponseWriter, r *http.Request) (scanUploadPayload, error) {
	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		return scanUploadPayload{}, errInvalidMultipartPayload
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return scanUploadPayload{}, errInvalidMultipartPayload
	}
	limitedBody := http.MaxBytesReader(w, r.Body, scanMaxImageBytes+scanMultipartOverheadBytes)
	defer limitedBody.Close()
	reader := multipart.NewReader(limitedBody, boundary)
	var out scanUploadPayload
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if isBodyTooLarge(err) {
				return scanUploadPayload{}, errScanPayloadTooLarge
			}
			return scanUploadPayload{}, errInvalidMultipartPayload
		}
		switch strings.TrimSpace(part.FormName()) {
		case "file":
			if out.Image != nil {
				return scanUploadPayload{}, errDuplicateScanFile
			}
			raw, err := io.ReadAll(io.LimitReader(part, scanMaxImageBytes+1))
			if err != nil {
				if isBodyTooLarge(err) {
					return scanUploadPayload{}, errScanPayloadTooLarge
				}
				return scanUploadPayload{}, errInvalidMultipartPayload
			}
			if len(raw) > scanMaxImageBytes {
				return scanUploadPayload{}, errScanPayloadTooLarge
			}
			mimeType := strings.TrimSpace(part.Header.Get("Content-Type"))
			if mimeType == "" {
				mimeType = http.DetectContentType(raw)
			}
			if _, _, err := image.DecodeConfig(bytes.NewReader(raw)); err != nil {
				return scanUploadPayload{}, errInvalidScanImage
			}
			out.Image = raw
			out.Filename = strings.TrimSpace(part.FileName())
			out.MIMEType = strings.TrimSpace(mimeType)
		case "project_id":
			value, err := io.ReadAll(io.LimitReader(part, scanMultipartFieldLimit))
			if err != nil {
				return scanUploadPayload{}, errInvalidMultipartPayload
			}
			out.ProjectID = strings.TrimSpace(string(value))
		case "item_id":
			id, err := readMultipartInt64(part)
			if err != nil {
				return scanUploadPayload{}, errInvalidMultipartPayload
			}
			out.ItemID = id
		case "artifact_id":
			id, err := readMultipartInt64(part)
			if err != nil {
				return scanUploadPayload{}, errInvalidMultipartPayload
			}
			out.ArtifactID = id
		default:
			if _, err := io.Copy(io.Discard, part); err != nil {
				return scanUploadPayload{}, errInvalidMultipartPayload
			}
		}
	}
	if out.Image == nil {
		return scanUploadPayload{}, errMissingScanFile
	}
	if out.Filename == "" {
		out.Filename = "scan"
	}
	return out, nil
}

func readMultipartInt64(part *multipart.Part) (int64, error) {
	value, err := io.ReadAll(io.LimitReader(part, scanMultipartFieldLimit))
	if err != nil {
		return 0, err
	}
	text := strings.TrimSpace(string(value))
	if text == "" {
		return 0, nil
	}
	return strconv.ParseInt(text, 10, 64)
}

func (a *App) extractScanAnnotations(ctx context.Context, req scanExtractRequest) (scanExtractResult, error) {
	if a != nil && a.scanExtractor != nil {
		return a.scanExtractor.ExtractScan(ctx, req)
	}
	baseURL := strings.TrimSpace(a.intentLLMURL)
	if baseURL == "" {
		return scanExtractResult{}, errScanExtractorOffline
	}
	body, _ := json.Marshal(map[string]any{
		"model":       a.localIntentLLMModel(),
		"temperature": 0,
		"max_tokens":  600,
		"response_format": map[string]any{
			"type": "json_object",
		},
		"chat_template_kwargs": map[string]any{
			"enable_thinking": false,
		},
		"messages": []map[string]any{
			{"role": "system", "content": scanExtractionSystemPrompt},
			{"role": "user", "content": []map[string]any{
				{"type": "text", "text": buildScanExtractionUserPrompt(req)},
				{"type": "image_url", "image_url": map[string]any{"url": "data:" + req.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(req.Image)}},
			}},
		},
	})
	requestCtx, cancel := context.WithTimeout(ctx, scanExtractorTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return scanExtractResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return scanExtractResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return scanExtractResult{}, fmt.Errorf("scan extractor HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var payload localIntentLLMChatCompletionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return scanExtractResult{}, err
	}
	if len(payload.Choices) == 0 {
		return scanExtractResult{}, nil
	}
	content := strings.TrimSpace(stripCodeFence(payload.Choices[0].Message.Content))
	if content == "" {
		return scanExtractResult{}, nil
	}
	var parsed scanLLMResponse
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return scanExtractResult{}, err
	}
	return scanExtractResult{
		Summary:     strings.TrimSpace(parsed.Summary),
		Annotations: sanitizeScanAnnotations(parsed.Annotations),
	}, nil
}

const scanExtractionSystemPrompt = `You extract handwritten annotations from scanned printouts.
Return strict JSON with shape:
{"summary":"short summary","annotations":[{"content":"required extracted note text","anchor_text":"printed source text nearest the note if known","line":42,"paragraph":3,"page":1,"bounds":{"x":0.1,"y":0.2,"width":0.3,"height":0.08},"confidence":0.9}]}

Rules:
- Prefer concise note text in content.
- Use line for printed code/text/diff documents when you can infer it.
- Use paragraph for prose/email documents when you can infer it from printed paragraph markers.
- Use page for PDF/page-based material when you can infer it.
- bounds must be normalized 0..1 relative to the original artifact page when known.
- Omit fields you cannot infer.
- If there are no handwritten annotations, return annotations as an empty array.`

func buildScanExtractionUserPrompt(req scanExtractRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Filename: %s\n", strings.TrimSpace(req.Filename))
	fmt.Fprintf(&b, "Mime type: %s\n", strings.TrimSpace(req.MIMEType))
	if req.Item != nil {
		fmt.Fprintf(&b, "Item title: %s\n", strings.TrimSpace(req.Item.Title))
	}
	if req.SourceArtifact != nil {
		fmt.Fprintf(&b, "Source artifact kind: %s\n", strings.TrimSpace(string(req.SourceArtifact.Kind)))
		fmt.Fprintf(&b, "Source artifact title: %s\n", strings.TrimSpace(optionalStringValue(req.SourceArtifact.Title)))
		if path := strings.TrimSpace(optionalStringValue(req.SourceArtifact.RefPath)); path != "" {
			fmt.Fprintf(&b, "Source artifact path: %s\n", path)
		}
	}
	if text := strings.TrimSpace(req.SourceText); text != "" {
		b.WriteString("\nPrinted source excerpt:\n")
		if len(text) > 6000 {
			text = text[:6000]
		}
		b.WriteString(text)
		b.WriteString("\n")
		b.WriteString("\nPrinted excerpt notes:\n")
		b.WriteString("- Machine-readable markers may be embedded inline, such as L001 for lines or P01 for paragraphs.\n")
		b.WriteString("- Prefer those markers when inferring line or paragraph positions.\n")
	}
	if meta := strings.TrimSpace(req.SourceMetaPretty); meta != "" {
		b.WriteString("\nSource metadata:\n")
		if len(meta) > 3000 {
			meta = meta[:3000]
		}
		b.WriteString(meta)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func sanitizeScanAnnotations(in []scanMappedAnnotation) []scanMappedAnnotation {
	out := make([]scanMappedAnnotation, 0, len(in))
	for _, entry := range in {
		content := strings.TrimSpace(entry.Content)
		if content == "" {
			continue
		}
		clean := scanMappedAnnotation{
			Content:    content,
			AnchorText: strings.TrimSpace(entry.AnchorText),
			Line:       maxInt(entry.Line, 0),
			Paragraph:  maxInt(entry.Paragraph, 0),
			Page:       maxInt(entry.Page, 0),
			Confidence: clampScanFloat(entry.Confidence),
		}
		if entry.Bounds != nil {
			clean.Bounds = &scanNormBounds{
				X:      clampScanFloat(entry.Bounds.X),
				Y:      clampScanFloat(entry.Bounds.Y),
				Width:  clampScanFloat(entry.Bounds.Width),
				Height: clampScanFloat(entry.Bounds.Height),
			}
		}
		out = append(out, clean)
	}
	return out
}

func buildScanUploadSummary(project store.Project, item *store.Item, artifact *store.Artifact, imagePath string, result scanExtractResult) string {
	lines := []string{
		"# Scan Import",
		"",
		fmt.Sprintf("- Project: `%s`", project.ID),
		fmt.Sprintf("- Scan image: `%s`", imagePath),
	}
	if item != nil {
		lines = append(lines, fmt.Sprintf("- Item: `%s`", item.Title))
	}
	if artifact != nil {
		lines = append(lines, fmt.Sprintf("- Source artifact: `%s`", strings.TrimSpace(optionalStringValue(artifact.Title))))
	}
	if summary := strings.TrimSpace(result.Summary); summary != "" {
		lines = append(lines, "", "## Summary", "", summary)
	}
	lines = append(lines, "", "## Extracted Annotations")
	if len(result.Annotations) == 0 {
		lines = append(lines, "", "- None detected")
		return strings.Join(lines, "\n")
	}
	for i, entry := range result.Annotations {
		line := fmt.Sprintf("%d. %s", i+1, entry.Content)
		if entry.Line > 0 {
			line += fmt.Sprintf(" (line %d)", entry.Line)
		} else if entry.Paragraph > 0 {
			line += fmt.Sprintf(" (paragraph %d)", entry.Paragraph)
		} else if entry.Page > 0 {
			line += fmt.Sprintf(" (page %d)", entry.Page)
		}
		lines = append(lines, "", line)
		if entry.AnchorText != "" {
			lines = append(lines, fmt.Sprintf("   - Anchor: `%s`", entry.AnchorText))
		}
	}
	return strings.Join(lines, "\n")
}

func buildScanConfirmSummary(project store.Project, item *store.Item, artifact *store.Artifact, annotations []scanMappedAnnotation) string {
	lines := []string{
		"# Reviewed Scan Annotations",
		"",
		fmt.Sprintf("- Project: `%s`", project.ID),
	}
	if item != nil {
		lines = append(lines, fmt.Sprintf("- Item: `%s`", item.Title))
	}
	if artifact != nil {
		lines = append(lines, fmt.Sprintf("- Source artifact: `%s`", strings.TrimSpace(optionalStringValue(artifact.Title))))
	}
	lines = append(lines, "", "## Final Annotations")
	for i, entry := range annotations {
		line := fmt.Sprintf("%d. %s", i+1, entry.Content)
		if entry.Line > 0 {
			line += fmt.Sprintf(" (line %d)", entry.Line)
		} else if entry.Paragraph > 0 {
			line += fmt.Sprintf(" (paragraph %d)", entry.Paragraph)
		} else if entry.Page > 0 {
			line += fmt.Sprintf(" (page %d)", entry.Page)
		}
		lines = append(lines, "", line)
		if entry.AnchorText != "" {
			lines = append(lines, fmt.Sprintf("   - Anchor: `%s`", entry.AnchorText))
		}
	}
	return strings.Join(lines, "\n")
}

func writeScanSummary(projectRoot, scanDir, stamp, baseName, content string) (string, error) {
	name := fmt.Sprintf("%s-%s.md", stamp, baseName)
	fullPath := filepath.Join(scanDir, name)
	if err := os.WriteFile(fullPath, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(projectRoot, fullPath)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func parseScanArtifactMeta(raw *string) (scanArtifactMeta, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return scanArtifactMeta{}, nil
	}
	var meta scanArtifactMeta
	err := json.Unmarshal([]byte(strings.TrimSpace(*raw)), &meta)
	return meta, err
}

func marshalScanMeta(meta scanArtifactMeta) (string, error) {
	encoded, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func extensionForScanMime(mimeType, filename string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	default:
		if ext := strings.ToLower(strings.TrimSpace(filepath.Ext(filename))); ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".png" {
			return ext
		}
		return ".png"
	}
}

func artifactTitlePtr(artifact *store.Artifact, item *store.Item) *string {
	if artifact != nil && strings.TrimSpace(optionalStringValue(artifact.Title)) != "" {
		return artifact.Title
	}
	if item == nil {
		return nil
	}
	return &item.Title
}

func itemIDValue(item *store.Item) int64 {
	if item == nil {
		return 0
	}
	return item.ID
}

func artifactIDValue(artifact *store.Artifact) int64 {
	if artifact == nil {
		return 0
	}
	return artifact.ID
}

func firstNonEmptyScan(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func scanStringPtr(value string) *string {
	text := strings.TrimSpace(value)
	if text == "" {
		return nil
	}
	return &text
}

func clampScanFloat(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
