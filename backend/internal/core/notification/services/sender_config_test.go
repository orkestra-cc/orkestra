package services

import (
	"reflect"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

func rosterValues() map[string]string {
	k := func(slug, sub string) string { return module.ItemKey(SendersField, slug, sub) }
	return map[string]string{
		module.RosterKey(SendersField):                  "mailup-sistema, esp-campagne",
		module.LabelKey(SendersField, "mailup-sistema"): "MailUp sistema",
		k("mailup-sistema", SubProvider):                "smtp",
		k("mailup-sistema", SubCategories):              " Auth.*,,*, auth.* ",
		k("mailup-sistema", SubFromAddress):             "sys@example.com",
		k("mailup-sistema", SubSMTPHost):                "relay",
		k("esp-campagne", SubProvider):                  "noop",
		k("esp-campagne", SubSMTPPort):                  "2525",
	}
}

func TestDecodeSenderProfiles_RosterOrderDefaultsAndNormalization(t *testing.T) {
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", testKeyHex) // encryptForTest: sender_loader_test.go
	encrypted := map[string]string{module.ItemKey(SendersField, "mailup-sistema", SubSMTPPassword): encryptForTest(t, "s3cret")}
	got, err := DecodeSenderProfiles(rosterValues(), encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Slug != "mailup-sistema" || got[1].Slug != "esp-campagne" {
		t.Fatalf("roster order lost: %+v", got)
	}
	a := got[0]
	if a.Label != "MailUp sistema" || a.Provider != "smtp" || a.FromAddress != "sys@example.com" || a.SMTPHost != "relay" {
		t.Fatalf("scalar fields: %+v", a)
	}
	if !reflect.DeepEqual(a.Categories, []string{"auth.*", "*"}) {
		t.Fatalf("categories must be normalized and deduplicated: %q", a.Categories)
	}
	if a.SMTPPort != 587 || a.SMTPTLSMode != "starttls" {
		t.Fatalf("absent sub-fields resolve to the item Default: port=%d tls=%q", a.SMTPPort, a.SMTPTLSMode)
	}
	if a.SMTPPassword != "s3cret" {
		t.Fatalf("secret must be decrypted from the snapshot's encrypted map, got %q", a.SMTPPassword)
	}
	b := got[1]
	if b.Provider != "noop" || b.SMTPPort != 2525 || len(b.Categories) != 0 || b.SMTPPassword != "" {
		t.Fatalf("second profile: %+v", b)
	}
}

func TestDecodeSenderProfiles_SaveTimeViewHasNoSecrets(t *testing.T) {
	got, err := DecodeSenderProfiles(rosterValues(), nil)
	if err != nil || got[0].SMTPPassword != "" {
		t.Fatalf("nil encrypted map must leave every secret empty: %v %+v", err, got)
	}
}

func TestDecodeSenderProfiles_UndecryptableSecretIsAnError(t *testing.T) {
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", testKeyHex)
	encrypted := map[string]string{module.ItemKey(SendersField, "mailup-sistema", SubSMTPPassword): "not-ciphertext"}
	if _, err := DecodeSenderProfiles(rosterValues(), encrypted); err == nil {
		t.Fatal("a secret that cannot be decrypted must fail the decode, never read as empty")
	}
}

func TestDecodeSenderProfiles_EmptyRoster(t *testing.T) {
	if got, err := DecodeSenderProfiles(map[string]string{}, nil); err != nil || len(got) != 0 {
		t.Fatalf("want empty, got %+v %v", got, err)
	}
	if got, err := DecodeSenderProfiles(nil, nil); err != nil || len(got) != 0 {
		t.Fatalf("nil map: %+v %v", got, err)
	}
}

func TestDecodeSenderProfiles_StaleEnumValueIsPreservedNotRejected(t *testing.T) {
	v := rosterValues()
	v[module.ItemKey(SendersField, "esp-campagne", SubProvider)] = "sendgrid"
	got, err := DecodeSenderProfiles(v, nil)
	if err != nil || len(got) != 2 || got[1].Provider != "sendgrid" {
		t.Fatalf("a provider the enum no longer lists must surface on its own profile as an unknown driver, not fail the decode: %+v %v", got, err)
	}
}

func TestSenderItems_DeclarationIsValid(t *testing.T) {
	field := module.ConfigField{Key: SendersField, Label: "Sender profiles", Type: module.FieldRecordList, Items: SenderItems()}
	if err := module.ValidateConfigDeclarations([]module.ConfigField{field}, nil); err != nil {
		t.Fatal(err)
	}
	// Every sub-field key is one SenderProfile.Field understands (or categories).
	for _, it := range SenderItems() {
		if it.Key == SubCategories {
			continue
		}
		var p SenderProfile
		p.setField(it.Key, "1")
		if p.Field(it.Key) == "" {
			t.Errorf("sub-field %q is not mapped by SenderProfile.Field/setField", it.Key)
		}
	}
}
