package mailtriage

import "time"

type Action string

const (
	ActionInbox   Action = "inbox"
	ActionCC      Action = "cc"
	ActionArchive Action = "archive"
	ActionTrash   Action = "trash"
)

type Phase string

const (
	PhaseShadow       Phase = "shadow"
	PhaseManualReview Phase = "manual_review"
	PhaseAutoApply    Phase = "auto_apply"
)

type Disposition string

const (
	DispositionShadow    Disposition = "shadow"
	DispositionReview    Disposition = "review"
	DispositionAutoApply Disposition = "auto_apply"
	DispositionNoop      Disposition = "noop"
)

type Message struct {
	ID             string    `json:"id"`
	Provider       string    `json:"provider,omitempty"`
	AccountLabel   string    `json:"account_label,omitempty"`
	AccountAddress string    `json:"account_address,omitempty"`
	ThreadID       string    `json:"thread_id,omitempty"`
	Subject        string    `json:"subject,omitempty"`
	Sender         string    `json:"sender,omitempty"`
	Recipients     []string  `json:"recipients,omitempty"`
	Labels         []string  `json:"labels,omitempty"`
	Snippet        string    `json:"snippet,omitempty"`
	Body           string    `json:"body,omitempty"`
	HasAttachments bool      `json:"has_attachments,omitempty"`
	IsRead         bool      `json:"is_read,omitempty"`
	IsFlagged      bool      `json:"is_flagged,omitempty"`
	ReceivedAt     time.Time `json:"received_at,omitempty"`
	Examples       []Example `json:"examples,omitempty"`
}

type Example struct {
	Sender  string `json:"sender,omitempty"`
	Subject string `json:"subject,omitempty"`
	Folder  string `json:"folder,omitempty"`
	Action  string `json:"action,omitempty"`
}

type Decision struct {
	Action       Action   `json:"action"`
	ArchiveLabel string   `json:"archive_label,omitempty"`
	Confidence   float64  `json:"confidence"`
	Reason       string   `json:"reason,omitempty"`
	Signals      []string `json:"signals,omitempty"`
	Model        string   `json:"model,omitempty"`
}

type Policy struct {
	Phase                     Phase              `json:"phase"`
	ReviewOnAuditDisagreement bool               `json:"review_on_audit_disagreement"`
	AutoApplyMinConfidence    map[Action]float64 `json:"auto_apply_min_confidence,omitempty"`
	ManualActions             []Action           `json:"manual_actions,omitempty"`
}

type Evaluation struct {
	Message        Message     `json:"message"`
	Primary        Decision    `json:"primary"`
	Audit          *Decision   `json:"audit,omitempty"`
	Disposition    Disposition `json:"disposition"`
	ReviewRequired bool        `json:"review_required"`
	ReviewReasons  []string    `json:"review_reasons,omitempty"`
}

func DefaultPolicy(phase Phase) Policy {
	if phase == "" {
		phase = PhaseManualReview
	}
	return Policy{
		Phase:                     phase,
		ReviewOnAuditDisagreement: true,
		AutoApplyMinConfidence: map[Action]float64{
			ActionCC:      0.90,
			ActionArchive: 0.93,
			ActionTrash:   0.98,
		},
	}
}
