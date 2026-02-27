package web

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/krystophny/tabura/internal/stt"
)

const sttReplacementsStateKey = "stt_replacements"

// defaultSTTReplacements seeds the replacement list from the former voxtype
// config (domain-specific physics terms and common Whisper misrecognitions).
var defaultSTTReplacements = map[string]string{
	"mating center":        "guiding center",
	"fast generation":      "fast gyration",
	"phase-based":          "phase-space",
	"Standards in practic": "Standard symplectic",
	"standards in practic": "standard symplectic",
	"rungicata":            "Runge-Kutta",
	"idiopatic":            "adiabatic",
	"pre-tunes":            "perturbations",
	"law statistics":       "loss statistics",
	"stelorators":          "stellarators",
	"stelorator":           "stellarator",
	"give meaning to":      "give me a link to",
	"from my site":         "from my side",
	"mark down files":      "markdown files",
}

func (a *App) handleSTTReplacementsGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	replacements := a.loadSTTReplacementsMap()
	writeJSON(w, replacements)
}

func (a *App) handleSTTReplacementsPut(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var replacements map[string]string
	if err := json.Unmarshal(body, &replacements); err != nil {
		http.Error(w, "invalid JSON: expected {\"from\": \"to\", ...}", http.StatusBadRequest)
		return
	}

	data, err := json.Marshal(replacements)
	if err != nil {
		http.Error(w, "failed to encode replacements", http.StatusInternalServerError)
		return
	}
	if err := a.store.SetAppState(sttReplacementsStateKey, string(data)); err != nil {
		http.Error(w, "failed to save replacements", http.StatusInternalServerError)
		return
	}
	writeJSON(w, replacements)
}

func (a *App) loadSTTReplacementsMap() map[string]string {
	raw, err := a.store.AppState(sttReplacementsStateKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return defaultSTTReplacements
	}
	var replacements map[string]string
	if err := json.Unmarshal([]byte(raw), &replacements); err != nil {
		return defaultSTTReplacements
	}
	return replacements
}

func (a *App) loadSTTReplacements() []stt.Replacement {
	m := a.loadSTTReplacementsMap()
	out := make([]stt.Replacement, 0, len(m))
	for from, to := range m {
		if strings.TrimSpace(from) == "" {
			continue
		}
		out = append(out, stt.Replacement{From: from, To: to})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From < out[j].From })
	return out
}
