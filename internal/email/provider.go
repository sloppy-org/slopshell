package email

import "context"

type EmailProvider interface {
	ListFolders(ctx context.Context) ([]Folder, error)
	ListMessages(ctx context.Context, opts ListMessageOptions) ([]Message, error)
	GetMessage(ctx context.Context, messageID string) (Message, error)
	ArchiveMessage(ctx context.Context, messageID string) error
	DeleteMessage(ctx context.Context, messageID string) error
	MarkRead(ctx context.Context, messageID string) error
	MarkUnread(ctx context.Context, messageID string) error
}

type ListMessageOptions struct {
	FolderID string
	Filter   string
	Select   []string
	Top      int
}

type Folder struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	WellKnownName    string `json:"wellKnownName"`
	ChildFolderCount int    `json:"childFolderCount"`
	TotalItemCount   int    `json:"totalItemCount"`
	UnreadItemCount  int    `json:"unreadItemCount"`
}

type Message struct {
	ID               string      `json:"id"`
	ConversationID   string      `json:"conversationId"`
	Subject          string      `json:"subject"`
	BodyPreview      string      `json:"bodyPreview"`
	IsRead           bool        `json:"isRead"`
	ParentFolderID   string      `json:"parentFolderId"`
	ReceivedDateTime string      `json:"receivedDateTime"`
	WebLink          string      `json:"webLink"`
	From             *Recipient  `json:"from,omitempty"`
	ToRecipients     []Recipient `json:"toRecipients,omitempty"`
	CcRecipients     []Recipient `json:"ccRecipients,omitempty"`
}

type Recipient struct {
	EmailAddress Address `json:"emailAddress"`
}

type Address struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}
