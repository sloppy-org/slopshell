package web

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
	tabsync "github.com/krystophny/tabura/internal/sync"
)

const emailThreadBindingObjectType = "email_thread"

type emailPersistedMessage struct {
	Message      *providerdata.EmailMessage
	Artifact     store.Artifact
	ItemID       *int64
	FollowUpItem bool
}

type emailThreadRecord struct {
	ThreadID    string
	Artifact    store.Artifact
	Messages    []emailPersistedMessage
	HasFollowUp bool
}

func emailThreadIDForMessage(message *providerdata.EmailMessage) string {
	if message == nil {
		return ""
	}
	threadID := strings.TrimSpace(message.ThreadID)
	if threadID != "" {
		return threadID
	}
	return strings.TrimSpace(message.ID)
}

func emailThreadTitle(messages []emailPersistedMessage) string {
	for _, message := range sortEmailMessagesByDate(messages) {
		subject := cleanEmailThreadSubject(message.Message)
		if subject != "" {
			return subject
		}
	}
	if len(messages) > 0 {
		if sender := strings.TrimSpace(messages[0].Message.Sender); sender != "" {
			return sender
		}
	}
	return "Email thread"
}

func cleanEmailThreadSubject(message *providerdata.EmailMessage) string {
	if message == nil {
		return ""
	}
	subject := strings.TrimSpace(message.Subject)
	for subject != "" {
		lower := strings.ToLower(subject)
		switch {
		case strings.HasPrefix(lower, "re:"):
			subject = strings.TrimSpace(subject[3:])
		case strings.HasPrefix(lower, "fw:"):
			subject = strings.TrimSpace(subject[3:])
		case strings.HasPrefix(lower, "fwd:"):
			subject = strings.TrimSpace(subject[4:])
		default:
			return subject
		}
	}
	return ""
}

func sortEmailMessagesByDate(messages []emailPersistedMessage) []emailPersistedMessage {
	out := append([]emailPersistedMessage(nil), messages...)
	sort.Slice(out, func(i, j int) bool {
		left := time.Time{}
		right := time.Time{}
		if out[i].Message != nil {
			left = out[i].Message.Date
		}
		if out[j].Message != nil {
			right = out[j].Message.Date
		}
		switch {
		case left.Equal(right):
			leftID := ""
			rightID := ""
			if out[i].Message != nil {
				leftID = strings.TrimSpace(out[i].Message.ID)
			}
			if out[j].Message != nil {
				rightID = strings.TrimSpace(out[j].Message.ID)
			}
			return leftID < rightID
		case left.IsZero():
			return false
		case right.IsZero():
			return true
		default:
			return left.After(right)
		}
	})
	return out
}

func emailThreadParticipants(messages []emailPersistedMessage) []string {
	seen := make(map[string]string)
	for _, persisted := range messages {
		if persisted.Message == nil {
			continue
		}
		for _, raw := range append([]string{persisted.Message.Sender}, persisted.Message.Recipients...) {
			participant := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
			if participant == "" {
				continue
			}
			key := strings.ToLower(participant)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = participant
		}
	}
	out := make([]string, 0, len(seen))
	for _, participant := range seen {
		out = append(out, participant)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func emailThreadRemoteUpdatedAt(messages []emailPersistedMessage) *string {
	for _, persisted := range sortEmailMessagesByDate(messages) {
		if persisted.Message == nil || persisted.Message.Date.IsZero() {
			continue
		}
		value := persisted.Message.Date.UTC().Format(time.RFC3339)
		return &value
	}
	return nil
}

func emailThreadContainerRef(messages []emailPersistedMessage, mappings []store.ExternalContainerMapping) *string {
	for _, persisted := range sortEmailMessagesByDate(messages) {
		if ref := emailMessageContainerRef(persisted.Message, mappings); ref != nil {
			return ref
		}
	}
	return nil
}

func emailThreadMetaJSON(threadID string, messages []emailPersistedMessage) (string, error) {
	messagePayload := make([]map[string]any, 0, len(messages))
	for _, persisted := range sortEmailMessagesByDate(messages) {
		if persisted.Message == nil {
			continue
		}
		record := map[string]any{
			"id":         strings.TrimSpace(persisted.Message.ID),
			"subject":    strings.TrimSpace(persisted.Message.Subject),
			"sender":     strings.TrimSpace(persisted.Message.Sender),
			"recipients": append([]string(nil), persisted.Message.Recipients...),
			"labels":     append([]string(nil), persisted.Message.Labels...),
			"is_read":    persisted.Message.IsRead,
		}
		if !persisted.Message.Date.IsZero() {
			record["date"] = persisted.Message.Date.UTC().Format(time.RFC3339)
		}
		if snippet := strings.TrimSpace(persisted.Message.Snippet); snippet != "" {
			record["snippet"] = snippet
		}
		if body := emailMessageBody(persisted.Message); body != "" {
			record["body"] = body
		}
		messagePayload = append(messagePayload, record)
	}
	payload := map[string]any{
		"thread_id":     strings.TrimSpace(threadID),
		"message_count": len(messages),
		"participants":  emailThreadParticipants(messages),
		"subject":       emailThreadTitle(messages),
		"messages":      messagePayload,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *App) persistEmailThreads(ctx context.Context, sink tabsync.Sink, account store.ExternalAccount, mappings []store.ExternalContainerMapping, messages []emailPersistedMessage) ([]emailThreadRecord, error) {
	grouped := make(map[string][]emailPersistedMessage)
	for _, persisted := range messages {
		threadID := emailThreadIDForMessage(persisted.Message)
		if threadID == "" {
			continue
		}
		grouped[threadID] = append(grouped[threadID], persisted)
	}
	if len(grouped) == 0 {
		return nil, nil
	}

	threadIDs := make([]string, 0, len(grouped))
	for threadID := range grouped {
		threadIDs = append(threadIDs, threadID)
	}
	sort.Strings(threadIDs)

	out := make([]emailThreadRecord, 0, len(threadIDs))
	for _, threadID := range threadIDs {
		group := grouped[threadID]
		title := emailThreadTitle(group)
		metaJSON, err := emailThreadMetaJSON(threadID, group)
		if err != nil {
			return nil, err
		}
		artifact, err := sink.UpsertArtifact(ctx, store.Artifact{
			Kind:     store.ArtifactKindEmailThread,
			Title:    &title,
			MetaJSON: &metaJSON,
		}, store.ExternalBinding{
			AccountID:       account.ID,
			Provider:        account.Provider,
			ObjectType:      emailThreadBindingObjectType,
			RemoteID:        threadID,
			ContainerRef:    emailThreadContainerRef(group, mappings),
			RemoteUpdatedAt: emailThreadRemoteUpdatedAt(group),
		})
		if err != nil {
			return nil, err
		}
		for _, persisted := range group {
			if persisted.ItemID == nil {
				continue
			}
			if err := a.store.LinkItemArtifact(*persisted.ItemID, artifact.ID, "related"); err != nil {
				return nil, err
			}
		}
		hasFollowUp := false
		for _, persisted := range group {
			if persisted.FollowUpItem {
				hasFollowUp = true
				break
			}
		}
		out = append(out, emailThreadRecord{
			ThreadID:    threadID,
			Artifact:    artifact,
			Messages:    sortEmailMessagesByDate(group),
			HasFollowUp: hasFollowUp,
		})
	}
	return out, nil
}
