package extensions

import "github.com/krystophny/tabura/internal/plugins"

const (
	HookChatPreUserMessage     = plugins.HookChatPreUserMessage
	HookChatPreAssistantPrompt = plugins.HookChatPreAssistantPrompt
	HookChatPostAssistantReply = plugins.HookChatPostAssistantReply
	HookMeetingPartnerSession  = plugins.HookMeetingPartnerSession
	HookMeetingPartnerSegment  = plugins.HookMeetingPartnerSegment
	HookMeetingPartnerDecide   = plugins.HookMeetingPartnerDecide
	HookExtensionCommand       = "extension.command"
)

const (
	PermissionHookChatPreUserMessage     = "hook.chat.pre_user_message"
	PermissionHookChatPreAssistantPrompt = "hook.chat.pre_assistant_prompt"
	PermissionHookChatPostAssistantReply = "hook.chat.post_assistant_response"
	PermissionMeetingPartnerSession      = "meeting_partner.session_state"
	PermissionMeetingPartnerSegment      = "meeting_partner.segment_finalized"
	PermissionMeetingPartnerDecide       = "meeting_partner.decide"
	PermissionCommandExecute             = "command.execute"
	PermissionUIContribute               = "ui.contribute"
	PermissionHookExecute                = "hook.execute"
)

type HookRequest = plugins.HookRequest

type HookResult = plugins.HookResult

type MeetingPartnerDecision = plugins.MeetingPartnerDecision

type CommandManifest struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Hook        string `json:"hook,omitempty"`
	Permission  string `json:"permission,omitempty"`
}

type UIContributionManifest struct {
	ID          string `json:"id"`
	Slot        string `json:"slot"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type EngineManifest struct {
	Tabura string `json:"tabura,omitempty"`
}

type SigningManifest struct {
	Publisher      string `json:"publisher,omitempty"`
	Signature      string `json:"signature,omitempty"`
	ChecksumSHA256 string `json:"checksum_sha256,omitempty"`
}

type Manifest struct {
	ID              string                   `json:"id"`
	DisplayName     string                   `json:"display_name,omitempty"`
	Version         string                   `json:"version,omitempty"`
	Kind            string                   `json:"kind,omitempty"`
	Endpoint        string                   `json:"endpoint,omitempty"`
	Hooks           []string                 `json:"hooks,omitempty"`
	Permissions     []string                 `json:"permissions,omitempty"`
	Commands        []CommandManifest        `json:"commands,omitempty"`
	UIContributions []UIContributionManifest `json:"ui_contributions,omitempty"`
	TimeoutMS       int                      `json:"timeout_ms,omitempty"`
	Enabled         bool                     `json:"enabled"`
	SecretEnv       string                   `json:"secret_env,omitempty"`
	Engine          EngineManifest           `json:"engine,omitempty"`
	Signing         SigningManifest          `json:"signing,omitempty"`
}

type CommandInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Hook        string `json:"hook"`
	Permission  string `json:"permission"`
}

type UIContributionInfo struct {
	ID          string `json:"id"`
	Slot        string `json:"slot"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type ExtensionInfo struct {
	ID              string               `json:"id"`
	DisplayName     string               `json:"display_name,omitempty"`
	Version         string               `json:"version"`
	Kind            string               `json:"kind"`
	Endpoint        string               `json:"endpoint,omitempty"`
	Hooks           []string             `json:"hooks,omitempty"`
	Permissions     []string             `json:"permissions,omitempty"`
	Commands        []CommandInfo        `json:"commands,omitempty"`
	UIContributions []UIContributionInfo `json:"ui_contributions,omitempty"`
	Enabled         bool                 `json:"enabled"`
	TimeoutMS       int                  `json:"timeout_ms"`
	Engine          EngineManifest       `json:"engine,omitempty"`
	Signing         SigningManifest      `json:"signing,omitempty"`
}

type CommandRequest struct {
	CommandID  string                 `json:"command_id"`
	SessionID  string                 `json:"session_id,omitempty"`
	ProjectKey string                 `json:"project_key,omitempty"`
	OutputMode string                 `json:"output_mode,omitempty"`
	Text       string                 `json:"text,omitempty"`
	Args       map[string]interface{} `json:"args,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type CommandResult struct {
	CommandID   string                 `json:"command_id"`
	ExtensionID string                 `json:"extension_id"`
	Success     bool                   `json:"success"`
	Message     string                 `json:"message,omitempty"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
}
