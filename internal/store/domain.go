package store

type ArtifactKind string

const (
	SphereWork    = "work"
	SpherePrivate = "private"

	ActorKindHuman = "human"
	ActorKindAgent = "agent"

	ArtifactKindEmail       ArtifactKind = "email"
	ArtifactKindDocument    ArtifactKind = "document"
	ArtifactKindPDF         ArtifactKind = "pdf"
	ArtifactKindMarkdown    ArtifactKind = "markdown"
	ArtifactKindImage       ArtifactKind = "image"
	ArtifactKindGitHubIssue ArtifactKind = "github_issue"
	ArtifactKindGitHubPR    ArtifactKind = "github_pr"
	ArtifactKindTranscript  ArtifactKind = "transcript"
	ArtifactKindPlanNote    ArtifactKind = "plan_note"
	ArtifactKindIdeaNote    ArtifactKind = "idea_note"

	ExternalProviderGmail          = "gmail"
	ExternalProviderIMAP           = "imap"
	ExternalProviderGoogleCalendar = "google_calendar"
	ExternalProviderICS            = "ics"
	ExternalProviderTodoist        = "todoist"
	ExternalProviderEvernote       = "evernote"
	ExternalProviderBear           = "bear"
	ExternalProviderExchange       = "exchange"

	ItemStateInbox   = "inbox"
	ItemStateWaiting = "waiting"
	ItemStateSomeday = "someday"
	ItemStateDone    = "done"
)

type ArtifactUpdate struct {
	Kind     *ArtifactKind `json:"kind,omitempty"`
	RefPath  *string       `json:"ref_path,omitempty"`
	RefURL   *string       `json:"ref_url,omitempty"`
	Title    *string       `json:"title,omitempty"`
	MetaJSON *string       `json:"meta_json,omitempty"`
}

type ItemUpdate struct {
	Title        *string `json:"title,omitempty"`
	State        *string `json:"state,omitempty"`
	WorkspaceID  *int64  `json:"workspace_id,omitempty"`
	ProjectID    *string `json:"project_id,omitempty"`
	Sphere       *string `json:"sphere,omitempty"`
	ArtifactID   *int64  `json:"artifact_id,omitempty"`
	ActorID      *int64  `json:"actor_id,omitempty"`
	VisibleAfter *string `json:"visible_after,omitempty"`
	FollowUpAt   *string `json:"follow_up_at,omitempty"`
	Source       *string `json:"source,omitempty"`
	SourceRef    *string `json:"source_ref,omitempty"`
}

type ItemOptions struct {
	State        string  `json:"state,omitempty"`
	WorkspaceID  *int64  `json:"workspace_id,omitempty"`
	ProjectID    *string `json:"project_id,omitempty"`
	Sphere       *string `json:"sphere,omitempty"`
	ArtifactID   *int64  `json:"artifact_id,omitempty"`
	ActorID      *int64  `json:"actor_id,omitempty"`
	VisibleAfter *string `json:"visible_after,omitempty"`
	FollowUpAt   *string `json:"follow_up_at,omitempty"`
	Source       *string `json:"source,omitempty"`
	SourceRef    *string `json:"source_ref,omitempty"`
}

type Workspace struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	DirPath   string `json:"dir_path"`
	Sphere    string `json:"sphere"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ExternalAccount struct {
	ID         int64  `json:"id"`
	Sphere     string `json:"sphere"`
	Provider   string `json:"provider"`
	Label      string `json:"label"`
	ConfigJSON string `json:"config_json"`
	Enabled    bool   `json:"enabled"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type ExternalAccountUpdate struct {
	Sphere   *string        `json:"sphere,omitempty"`
	Provider *string        `json:"provider,omitempty"`
	Label    *string        `json:"label,omitempty"`
	Config   map[string]any `json:"config,omitempty"`
	Enabled  *bool          `json:"enabled,omitempty"`
}

type ExternalBinding struct {
	ID              int64   `json:"id"`
	AccountID       int64   `json:"account_id"`
	Provider        string  `json:"provider"`
	ObjectType      string  `json:"object_type"`
	RemoteID        string  `json:"remote_id"`
	ItemID          *int64  `json:"item_id,omitempty"`
	ArtifactID      *int64  `json:"artifact_id,omitempty"`
	ContainerRef    *string `json:"container_ref,omitempty"`
	RemoteUpdatedAt *string `json:"remote_updated_at,omitempty"`
	LastSyncedAt    string  `json:"last_synced_at"`
}

type ExternalContainerMapping struct {
	ID            int64   `json:"id"`
	Provider      string  `json:"provider"`
	ContainerType string  `json:"container_type"`
	ContainerRef  string  `json:"container_ref"`
	WorkspaceID   *int64  `json:"workspace_id,omitempty"`
	ProjectID     *string `json:"project_id,omitempty"`
	Sphere        *string `json:"sphere,omitempty"`
}

type ArtifactWorkspaceLink struct {
	WorkspaceID int64  `json:"workspace_id"`
	ArtifactID  int64  `json:"artifact_id"`
	CreatedAt   string `json:"created_at"`
}

type ItemArtifactLink struct {
	ItemID     int64  `json:"item_id"`
	ArtifactID int64  `json:"artifact_id"`
	Role       string `json:"role"`
	CreatedAt  string `json:"created_at"`
}

type ItemArtifact struct {
	ItemID        int64    `json:"item_id"`
	ArtifactID    int64    `json:"artifact_id"`
	Role          string   `json:"role"`
	LinkCreatedAt string   `json:"link_created_at"`
	Artifact      Artifact `json:"artifact"`
}

type Actor struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
}

type Artifact struct {
	ID        int64        `json:"id"`
	Kind      ArtifactKind `json:"kind"`
	RefPath   *string      `json:"ref_path,omitempty"`
	RefURL    *string      `json:"ref_url,omitempty"`
	Title     *string      `json:"title,omitempty"`
	MetaJSON  *string      `json:"meta_json,omitempty"`
	CreatedAt string       `json:"created_at"`
	UpdatedAt string       `json:"updated_at"`
}

type Item struct {
	ID           int64   `json:"id"`
	Title        string  `json:"title"`
	State        string  `json:"state"`
	WorkspaceID  *int64  `json:"workspace_id,omitempty"`
	ProjectID    *string `json:"project_id,omitempty"`
	Sphere       string  `json:"sphere"`
	ArtifactID   *int64  `json:"artifact_id,omitempty"`
	ActorID      *int64  `json:"actor_id,omitempty"`
	VisibleAfter *string `json:"visible_after,omitempty"`
	FollowUpAt   *string `json:"follow_up_at,omitempty"`
	Source       *string `json:"source,omitempty"`
	SourceRef    *string `json:"source_ref,omitempty"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type ItemSummary struct {
	Item
	ArtifactTitle *string       `json:"artifact_title,omitempty"`
	ArtifactKind  *ArtifactKind `json:"artifact_kind,omitempty"`
	ActorName     *string       `json:"actor_name,omitempty"`
}
