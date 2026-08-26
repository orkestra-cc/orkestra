package services

import (
	"fmt"
	"strings"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// SendersField is the record-list key that holds sender profiles.
const SendersField = "email.senders"

// SenderItems is the element schema of email.senders. It lives next to the
// decoder that reads it and module.go's ConfigSchema references it, so the
// two cannot drift.
//
// Every Required is scoped by the DependsOn that governs its driver — a
// correctness rule, not tidiness: the console blocks Save on an empty
// visible required field, so from_address required outside smtp would
// make a noop profile unsavable.
func SenderItems() []module.ConfigItemField {
	smtpOnly := []module.FieldCondition{{Key: SubProvider, In: []string{"smtp"}}}
	identity := []module.FieldCondition{{Key: SubProvider, In: []string{"smtp"}}}
	return []module.ConfigItemField{
		{Key: SubProvider, Label: "Provider", Type: module.FieldEnum, Options: []string{"noop", "smtp"}, Required: true, Default: "noop"},
		{Key: SubCategories, Label: "Categories", Type: module.FieldStringList, Placeholder: "auth.*, *",
			Description: "Routing patterns this profile serves: an exact category (auth.verify_email), a prefix (auth.*), or * for the default. Leave empty to keep the profile as a draft that receives no mail."},
		{Key: SubFromAddress, Label: "From address", Type: module.FieldString, Required: true, DependsOn: identity},
		{Key: SubFromName, Label: "From name", Type: module.FieldString, DependsOn: identity},
		{Key: SubReplyTo, Label: "Reply-To address", Type: module.FieldString, DependsOn: identity},
		{Key: SubSMTPHost, Label: "SMTP host", Type: module.FieldString, Required: true, DependsOn: smtpOnly},
		{Key: SubSMTPPort, Label: "SMTP port", Type: module.FieldInt, Default: "587", DependsOn: smtpOnly},
		{Key: SubSMTPTLSMode, Label: "TLS mode", Type: module.FieldEnum, Options: []string{"starttls", "tls", "none"}, Default: "starttls", DependsOn: smtpOnly},
		{Key: SubSMTPUsername, Label: "SMTP username", Type: module.FieldString, DependsOn: smtpOnly,
			Description: "Leave username and password empty for an unauthenticated relay."},
		{Key: SubSMTPPassword, Label: "SMTP password", Type: module.FieldSecret, DependsOn: smtpOnly},
	}
}

// DecodeSenderProfiles reads every element of email.senders out of ONE
// snapshot — the flat value map and the encrypted map of the same
// environment — in roster order. encrypted == nil is the save-time view, in
// which every secret reads "". A secret that cannot be decrypted is an
// error: the loader fails closed on it rather than sending with an empty
// password.
//
// Sub-field resolution is stored value → item Default, and the provider
// enum is NOT enforced here: a stale value must surface as ErrUnknownDriver
// on the profile that carries it, never as a decode failure that takes
// every other profile down with it.
func DecodeSenderProfiles(values, encrypted map[string]string) ([]SenderProfile, error) {
	items := SenderItems()
	roster := module.ParseRoster(values, SendersField)
	out := make([]SenderProfile, 0, len(roster))
	for _, slug := range roster {
		p := SenderProfile{Slug: slug, Label: values[module.LabelKey(SendersField, slug)]}
		for _, it := range items {
			key := module.ItemKey(SendersField, slug, it.Key)
			var v string
			switch {
			case it.Type == module.FieldSecret:
				plain, err := decryptSnapshotSecret(encrypted[key])
				if err != nil {
					return nil, fmt.Errorf("notification: sender %q: decrypt %s: %w", slug, it.Key, err)
				}
				v = plain
			default:
				v = strings.TrimSpace(values[key])
				if v == "" {
					v = it.Default
				}
			}
			switch it.Key {
			case SubCategories:
				p.Categories = NormalizePatterns(strings.Split(v, ","))
			case SubProvider:
				p.setField(it.Key, strings.ToLower(v))
			default:
				p.setField(it.Key, v)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// decryptSnapshotSecret decrypts one stored ciphertext through the SDK's
// exported decoder — the same AES-256-GCM path GetSecret uses — without a
// repository read. "" decrypts to "".
func decryptSnapshotSecret(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	var v struct {
		S string `module:"s"`
	}
	schema := []module.ConfigField{{Key: "s", Type: module.FieldSecret}}
	if err := module.UnmarshalConfig(schema, nil, map[string]string{"s": enc}, &v); err != nil {
		return "", err
	}
	return v.S, nil
}
