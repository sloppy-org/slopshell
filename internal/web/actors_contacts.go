package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

type contactSyncProvider interface {
	ListContacts(context.Context) ([]providerdata.Contact, error)
	Close() error
}

func (a *App) contactSyncProviderForAccount(ctx context.Context, account store.ExternalAccount) (contactSyncProvider, error) {
	if a != nil && a.newContactSyncProvider != nil {
		return a.newContactSyncProvider(ctx, account)
	}
	switch account.Provider {
	case store.ExternalProviderGmail:
		cfg, err := decodeEmailSyncAccountConfig(account)
		if err != nil {
			return nil, err
		}
		return email.NewGmailWithFiles(gmailCredentialsPathForAccount(cfg), gmailTokenPathForAccount(account, cfg))
	case store.ExternalProviderExchange:
		cfg, err := decodeExchangeContactAccountConfig(account)
		if err != nil {
			return nil, err
		}
		return email.NewExchangeClient(cfg)
	default:
		return nil, fmt.Errorf("contact sync does not support provider %s", account.Provider)
	}
}

func decodeExchangeContactAccountConfig(account store.ExternalAccount) (email.ExchangeConfig, error) {
	config := map[string]any{}
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			return email.ExchangeConfig{}, fmt.Errorf("decode exchange contact config: %w", err)
		}
	}
	return email.ExchangeConfigFromMap(account.Label, config)
}

func (a *App) syncContactAccount(ctx context.Context, account store.ExternalAccount) (int, error) {
	provider, err := a.contactSyncProviderForAccount(ctx, account)
	if err != nil {
		return 0, err
	}
	defer provider.Close()
	return a.syncContactAccountWithProvider(ctx, account, provider)
}

func (a *App) syncContactAccountWithProvider(ctx context.Context, account store.ExternalAccount, provider contactSyncProvider) (int, error) {
	contacts, err := provider.ListContacts(ctx)
	if err != nil {
		return 0, err
	}
	for _, contact := range contacts {
		if _, err := a.upsertContactActor(account, contact); err != nil {
			return 0, err
		}
	}
	return len(contacts), nil
}

func (a *App) upsertContactActor(account store.ExternalAccount, contact providerdata.Contact) (*store.Actor, error) {
	emailAddress := normalizeSenderEmail(contact.Email)
	name := strings.TrimSpace(contact.Name)
	if name == "" {
		name = emailAddress
	}
	if name == "" {
		return nil, nil
	}
	metaJSON, err := actorContactMetaJSON(contact)
	if err != nil {
		return nil, err
	}
	actor, err := a.store.UpsertActorContact(name, emailAddress, account.Provider, contact.ProviderRef, metaJSON)
	if err != nil {
		return nil, err
	}
	return &actor, nil
}

func actorContactMetaJSON(contact providerdata.Contact) (*string, error) {
	payload := map[string]any{}
	if organization := strings.TrimSpace(contact.Organization); organization != "" {
		payload["organization"] = organization
	}
	phones := make([]string, 0, len(contact.Phones))
	for _, phone := range contact.Phones {
		phone = strings.TrimSpace(phone)
		if phone != "" {
			phones = append(phones, phone)
		}
	}
	if len(phones) > 0 {
		payload["phones"] = phones
	}
	if len(payload) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	value := string(raw)
	return &value, nil
}

func parseSenderContact(sender string) providerdata.Contact {
	clean := strings.TrimSpace(sender)
	if clean == "" {
		return providerdata.Contact{}
	}
	if parsed, err := mail.ParseAddress(clean); err == nil {
		return providerdata.Contact{
			Name:  strings.TrimSpace(parsed.Name),
			Email: normalizeSenderEmail(parsed.Address),
		}
	}
	if addressList, err := mail.ParseAddressList(clean); err == nil && len(addressList) > 0 {
		return providerdata.Contact{
			Name:  strings.TrimSpace(addressList[0].Name),
			Email: normalizeSenderEmail(addressList[0].Address),
		}
	}
	if strings.Contains(clean, "@") && !strings.ContainsAny(clean, " ,;") {
		return providerdata.Contact{Email: normalizeSenderEmail(clean), Name: normalizeSenderEmail(clean)}
	}
	return providerdata.Contact{Name: clean}
}

func normalizeSenderEmail(emailAddress string) string {
	return strings.ToLower(strings.TrimSpace(emailAddress))
}
