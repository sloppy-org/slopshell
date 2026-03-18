package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

func TestTextPolishAPI(t *testing.T) {
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		messages, _ := req["messages"].([]any)
		if len(messages) < 2 {
			http.Error(w, "need messages", http.StatusBadRequest)
			return
		}
		userMsg, _ := messages[1].(map[string]any)
		userContent := fmt.Sprint(userMsg["content"])
		polished := strings.ReplaceAll(userContent, "Muenchen", "München")
		polished = strings.ReplaceAll(polished, "schoene", "schöne")
		resp := fmt.Sprintf(`{"polished_body":%q}`, polished)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": resp}},
			},
		})
	}))
	defer mockLLM.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = mockLLM.URL
	app.intentLLMModel = "test-model"

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/text/polish", map[string]any{
		"body": "Hallo aus Muenchen, schoene Stadt",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	data := decodeJSONDataResponse(t, rr)
	polished, _ := data["polished_body"].(string)
	if !strings.Contains(polished, "München") {
		t.Fatalf("polished = %q, want München", polished)
	}
	if !strings.Contains(polished, "schöne") {
		t.Fatalf("polished = %q, want schöne", polished)
	}
}

func TestTextPolishEmptyBody(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/text/polish", map[string]any{
		"body": "",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestTextPolishFallbackWhenLLMOffline(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/text/polish", map[string]any{
		"body": "raw dictation text",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	data := decodeJSONDataResponse(t, rr)
	polished, _ := data["polished_body"].(string)
	if polished != "raw dictation text" {
		t.Fatalf("polished = %q, want original body when LLM offline", polished)
	}
}

func TestTextPolishFallbackWhenLLMFails(t *testing.T) {
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer mockLLM.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = mockLLM.URL

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/text/polish", map[string]any{
		"body": "raw dictation text",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	data := decodeJSONDataResponse(t, rr)
	polished, _ := data["polished_body"].(string)
	if polished != "raw dictation text" {
		t.Fatalf("polished = %q, want original body as fallback", polished)
	}
}

func TestMailDraftPolishAPI(t *testing.T) {
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		messages, _ := req["messages"].([]any)
		if len(messages) < 2 {
			http.Error(w, "need messages", http.StatusBadRequest)
			return
		}
		userMsg, _ := messages[1].(map[string]any)
		userContent := fmt.Sprint(userMsg["content"])
		if !strings.Contains(userContent, "Original email:") {
			t.Errorf("user prompt missing original email context: %q", userContent)
		}
		polished := "Sehr geehrter Herr Schmidt, vielen Dank für Ihre Nachricht."
		resp := fmt.Sprintf(`{"polished_body":%q}`, polished)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": resp}},
			},
		})
	}))
	defer mockLLM.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = mockLLM.URL
	app.intentLLMModel = "test-model"
	mustCreateProject(t, app)

	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Test Gmail", map[string]any{
		"username": "me@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	provider := &fakeMailDraftProvider{
		message: &providerdata.EmailMessage{
			ID:      "original-msg-1",
			Subject: "Quarterly Report",
			Sender:  "Schmidt <schmidt@example.com>",
		},
	}
	app.newEmailProvider = func(ctx2 context.Context, acc store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts/reply", map[string]any{
		"item_id": seedMailItem(t, app, account, "original-msg-1", "Quarterly Report", "Schmidt <schmidt@example.com>"),
	})
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create reply status = %d, want 201: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createData := decodeJSONDataResponse(t, rrCreate)["draft"].(map[string]any)
	artifactID := fmt.Sprintf("%d", int64(createData["artifact_id"].(float64)))

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts/"+artifactID+"/polish", map[string]any{
		"body": "ja herr schmidt danke fuer die nachricht",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("polish status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	data := decodeJSONDataResponse(t, rr)
	polished, _ := data["polished_body"].(string)
	if !strings.Contains(polished, "Schmidt") {
		t.Fatalf("polished = %q, want name from original email", polished)
	}
}

func TestMailDraftPolishNewEmail(t *testing.T) {
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		messages, _ := req["messages"].([]any)
		userMsg, _ := messages[1].(map[string]any)
		userContent := fmt.Sprint(userMsg["content"])
		if strings.Contains(userContent, "Original email:") {
			t.Errorf("new email polish should not have original email context")
		}
		polished := "Polished new email text."
		resp := fmt.Sprintf(`{"polished_body":%q}`, polished)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": resp}},
			},
		})
	}))
	defer mockLLM.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = mockLLM.URL
	app.intentLLMModel = "test-model"
	mustCreateProject(t, app)

	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Test Gmail", map[string]any{
		"username": "me@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	app.newEmailProvider = func(ctx2 context.Context, acc store.ExternalAccount) (email.EmailProvider, error) {
		return &fakeMailDraftProvider{}, nil
	}

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts", map[string]any{
		"account_id": account.ID,
	})
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create draft status = %d, want 201: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createData := decodeJSONDataResponse(t, rrCreate)["draft"].(map[string]any)
	artifactID := fmt.Sprintf("%d", int64(createData["artifact_id"].(float64)))

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts/"+artifactID+"/polish", map[string]any{
		"body": "hallo welt",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("polish status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	data := decodeJSONDataResponse(t, rr)
	polished, _ := data["polished_body"].(string)
	if polished == "" {
		t.Fatal("polished body is empty")
	}
}

func seedMailItem(t *testing.T, app *App, account store.ExternalAccount, messageID, subject, sender string) int64 {
	t.Helper()
	metaJSON := fmt.Sprintf(`{"subject":%q,"sender":%q,"body":"Original email body content here.","thread_id":"thread-1"}`, subject, sender)
	title := subject
	artifact, err := app.store.CreateArtifact(store.ArtifactKindEmail, &messageID, nil, &title, &metaJSON)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	containerRef := "INBOX"
	_, err = app.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:    account.ID,
		Provider:     account.Provider,
		ObjectType:   "email",
		RemoteID:     messageID,
		ArtifactID:   &artifact.ID,
		ContainerRef: &containerRef,
	})
	if err != nil {
		t.Fatalf("UpsertExternalBinding() error: %v", err)
	}
	source := account.Provider
	sourceRef := "message:" + messageID
	artifactID := artifact.ID
	item, err := app.store.CreateItem(title, store.ItemOptions{
		ArtifactID: &artifactID,
		Source:     &source,
		SourceRef:  &sourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	return item.ID
}
