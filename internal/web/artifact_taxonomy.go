package web

import (
	"encoding/json"
	"net/http"
	"strings"
)

type canonicalActionSpec struct {
	Label       string `json:"label"`
	PromptLabel string `json:"prompt_label"`
	Description string `json:"description"`
}

type artifactKindSpec struct {
	Family           string   `json:"family"`
	CanvasSurface    string   `json:"canvas_surface"`
	InteractionModel string   `json:"interaction_model"`
	Actions          []string `json:"actions"`
	MailActions      bool     `json:"mail_actions"`
}

type artifactTaxonomyPayload struct {
	CanonicalActionOrder []string                       `json:"canonical_action_order"`
	Actions              map[string]canonicalActionSpec `json:"actions"`
	Kinds                map[string]artifactKindSpec    `json:"kinds"`
}

var artifactTaxonomy = artifactTaxonomyPayload{
	CanonicalActionOrder: []string{
		"open_show",
		"annotate_capture",
		"compose",
		"bundle_review",
		"dispatch_execute",
		"track_item",
		"delegate_actor",
	},
	Actions: map[string]canonicalActionSpec{
		"open_show": {
			Label:       "Open",
			PromptLabel: "Open / Show",
			Description: "Inspect or surface the artifact in context.",
		},
		"annotate_capture": {
			Label:       "Annotate",
			PromptLabel: "Annotate / Capture",
			Description: "Mark up the artifact or capture observations from it.",
		},
		"compose": {
			Label:       "Compose",
			PromptLabel: "Compose",
			Description: "Draft new content in response to the artifact.",
		},
		"bundle_review": {
			Label:       "Review",
			PromptLabel: "Bundle / Review",
			Description: "Gather notes, compare context, and review before action.",
		},
		"dispatch_execute": {
			Label:       "Dispatch",
			PromptLabel: "Dispatch / Execute",
			Description: "Send, apply, or execute the prepared result.",
		},
		"track_item": {
			Label:       "Track",
			PromptLabel: "Track as Item",
			Description: "Track follow-up work as an item.",
		},
		"delegate_actor": {
			Label:       "Delegate",
			PromptLabel: "Delegate to Actor",
			Description: "Hand the work to a specific actor.",
		},
	},
	Kinds: map[string]artifactKindSpec{
		"annotation": {
			Family:           "review_bundle",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "bundle_review", "dispatch_execute", "track_item"},
		},
		"document": {
			Family:           "reference",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "bundle_review", "track_item"},
		},
		"email": {
			Family:           "message",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "compose", "dispatch_execute", "track_item"},
			MailActions:      true,
		},
		"email_thread": {
			Family:           "message",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "compose", "bundle_review", "dispatch_execute", "track_item"},
			MailActions:      true,
		},
		"external_note": {
			Family:           "captured_note",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "compose", "track_item"},
		},
		"external_task": {
			Family:           "action_card",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "compose", "dispatch_execute", "track_item", "delegate_actor"},
		},
		"github_issue": {
			Family:           "proposal",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "compose", "bundle_review", "dispatch_execute", "track_item"},
		},
		"github_pr": {
			Family:           "proposal",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "bundle_review", "dispatch_execute", "track_item", "delegate_actor"},
		},
		"idea_note": {
			Family:           "planning_note",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "compose", "bundle_review", "track_item"},
		},
		"image": {
			Family:           "reference",
			CanvasSurface:    "image_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "bundle_review", "track_item"},
		},
		"markdown": {
			Family:           "reference",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "bundle_review", "track_item"},
		},
		"pdf": {
			Family:           "reference",
			CanvasSurface:    "pdf_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "bundle_review", "track_item"},
		},
		"plan_note": {
			Family:           "planning_note",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "compose", "bundle_review", "track_item"},
		},
		"reference": {
			Family:           "reference",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "bundle_review", "track_item"},
		},
		"transcript": {
			Family:           "transcript",
			CanvasSurface:    "text_artifact",
			InteractionModel: "canonical_canvas",
			Actions:          []string{"open_show", "annotate_capture", "bundle_review", "track_item"},
		},
	},
}

var defaultArtifactKindSpec = artifactKindSpec{
	Family:           "artifact",
	CanvasSurface:    "text_artifact",
	InteractionModel: "canonical_canvas",
	Actions:          []string{"open_show", "annotate_capture", "compose", "bundle_review", "track_item"},
}

func normalizedArtifactKind(kind string) string {
	return strings.TrimSpace(strings.ToLower(kind))
}

func lookupArtifactKindSpec(kind string) artifactKindSpec {
	spec, ok := artifactTaxonomy.Kinds[normalizedArtifactKind(kind)]
	if ok {
		return spec
	}
	return defaultArtifactKindSpec
}

func artifactPromptLabel(action string) string {
	spec, ok := artifactTaxonomy.Actions[strings.TrimSpace(action)]
	if !ok {
		return strings.TrimSpace(action)
	}
	return spec.PromptLabel
}

func artifactPromptActions(kind string) []string {
	spec := lookupArtifactKindSpec(kind)
	out := make([]string, 0, len(spec.Actions))
	for _, action := range spec.Actions {
		label := artifactPromptLabel(action)
		if label != "" {
			out = append(out, label)
		}
	}
	return out
}

func marshalArtifactTaxonomy() []byte {
	data, err := json.Marshal(artifactTaxonomy)
	if err != nil {
		return []byte(`{"canonical_action_order":[],"actions":{},"kinds":{}}`)
	}
	return data
}

func (a *App) handleArtifactTaxonomyGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSONStatus(w, http.StatusOK, artifactTaxonomy)
}
