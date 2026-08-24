package module

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// svcWithProfileSchema stamps a record-list declaration onto the fixture. The
// validator walks the SCHEMA, so a fixture without one exercises nothing.
func svcWithProfileSchema(t *testing.T, values map[string]string) (*ModuleConfigService, *fakeConfigRepo) {
	t.Helper()
	svc, repo := svcWith(t, values, nil)
	repo.docs["demo"].ConfigSchema = []ConfigField{{
		Key:   "email.profiles",
		Label: "Delivery profiles",
		Type:  FieldRecordList,
		Items: []ConfigItemField{{Key: "host", Label: "Host", Type: FieldString}},
	}}
	return svc, repo
}

// createProfile submits one creation with whatever label values the caller
// supplies, mirroring how the console composes a create: the element's values
// travel in the ordinary map, the membership intent in the mutation.
func createProfile(t *testing.T, svc *ModuleConfigService, slug string, values map[string]string) error {
	t.Helper()
	return svc.UpdateEnvironmentConfigWithRecordLists(
		context.Background(), "demo", "production",
		values, nil,
		[]RecordListMutation{{Field: "email.profiles", Create: []string{slug}}},
		nil,
	)
}

func TestCreateRequiresALabel(t *testing.T) {
	svc, _ := svcWithProfileSchema(t, map[string]string{})

	err := createProfile(t, svc, "mailup-smtp", map[string]string{
		"email.profiles.mailup-smtp.host": "smtp.mailup.it",
	})
	if !errors.Is(err, ErrLabelRequired) {
		t.Fatalf("creating without a label: got %v, want ErrLabelRequired", err)
	}
}

func TestCreateRejectsABlankLabel(t *testing.T) {
	svc, _ := svcWithProfileSchema(t, map[string]string{})

	err := createProfile(t, svc, "mailup-smtp", map[string]string{
		LabelKey("email.profiles", "mailup-smtp"): "   ",
	})
	if !errors.Is(err, ErrLabelRequired) {
		t.Fatalf("blank label: got %v, want ErrLabelRequired", err)
	}
}

func TestCreateRejectsAnOverlongLabel(t *testing.T) {
	svc, _ := svcWithProfileSchema(t, map[string]string{})
	long := strings.Repeat("x", MaxLabelLength+1)

	err := createProfile(t, svc, MintSlug(long), map[string]string{
		LabelKey("email.profiles", MintSlug(long)): long,
	})
	if !errors.Is(err, ErrLabelTooLong) {
		t.Fatalf("over-long label: got %v, want ErrLabelTooLong", err)
	}
}

// The readable key must not lie about the record it names. Without this, a
// client can create "mailup-smtp" labelled "SendGrid bulk".
func TestCreateBindsTheSlugToItsLabel(t *testing.T) {
	svc, _ := svcWithProfileSchema(t, map[string]string{})

	err := createProfile(t, svc, "mailup-smtp", map[string]string{
		LabelKey("email.profiles", "mailup-smtp"): "SendGrid bulk",
	})
	if !errors.Is(err, ErrSlugLabelMismatch) {
		t.Fatalf("mismatched slug and label: got %v, want ErrSlugLabelMismatch", err)
	}
}

func TestCreateAcceptsASlugMintedFromItsLabel(t *testing.T) {
	svc, repo := svcWithProfileSchema(t, map[string]string{})

	if err := createProfile(t, svc, "mailup-smtp", map[string]string{
		LabelKey("email.profiles", "mailup-smtp"): "MailUp SMTP+",
		"email.profiles.mailup-smtp.host":         "smtp.mailup.it",
	}); err != nil {
		t.Fatalf("a well-formed creation was rejected: %v", err)
	}

	got := repo.docs["demo"].Environments["production"].ConfigValues
	if got[RosterKey("email.profiles")] != "mailup-smtp" {
		t.Fatalf("roster = %q", got[RosterKey("email.profiles")])
	}
	if got[LabelKey("email.profiles", "mailup-smtp")] != "MailUp SMTP+" {
		t.Fatalf("label not stored: %q", got[LabelKey("email.profiles", "mailup-smtp")])
	}
}

// The binding holds at creation and NOWHERE else: the label stays editable,
// the slug stays frozen, and the two are expected to diverge (D2).
func TestRenameLeavesTheSlugAloneAndIsNotRebound(t *testing.T) {
	svc, repo := svcWithProfileSchema(t, map[string]string{
		RosterKey("email.profiles"):               "mailup-smtp",
		LabelKey("email.profiles", "mailup-smtp"): "MailUp SMTP+",
	})

	err := svc.UpdateEnvironmentConfigWithRecordLists(
		context.Background(), "demo", "production",
		map[string]string{LabelKey("email.profiles", "mailup-smtp"): "Completely different name"},
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("rename rejected: %v", err)
	}

	got := repo.docs["demo"].Environments["production"].ConfigValues
	if got[RosterKey("email.profiles")] != "mailup-smtp" {
		t.Fatalf("rename moved the slug: roster = %q", got[RosterKey("email.profiles")])
	}
	if got[LabelKey("email.profiles", "mailup-smtp")] != "Completely different name" {
		t.Fatalf("rename did not take: %q", got[LabelKey("email.profiles", "mailup-smtp")])
	}
}

func TestRenameStillObeysTheLabelRules(t *testing.T) {
	svc, _ := svcWithProfileSchema(t, map[string]string{
		RosterKey("email.profiles"):               "mailup-smtp",
		LabelKey("email.profiles", "mailup-smtp"): "MailUp SMTP+",
	})

	err := svc.UpdateEnvironmentConfigWithRecordLists(
		context.Background(), "demo", "production",
		map[string]string{LabelKey("email.profiles", "mailup-smtp"): ""},
		nil, nil, nil,
	)
	if !errors.Is(err, ErrLabelRequired) {
		t.Fatalf("blanking a label: got %v, want ErrLabelRequired", err)
	}
}

// A write aimed at an element that does not exist is a client acting on a view
// of the world that no longer holds — not an instruction to create one.
func TestValueForAnUnknownSlugIsRejected(t *testing.T) {
	svc, _ := svcWithProfileSchema(t, map[string]string{
		RosterKey("email.profiles"): "mailup-smtp",
	})

	err := svc.UpdateEnvironmentConfigWithRecordLists(
		context.Background(), "demo", "production",
		map[string]string{"email.profiles.ghost.host": "nowhere.example"},
		nil, nil, nil,
	)
	if !errors.Is(err, ErrUnknownSlug) {
		t.Fatalf("value for an absent slug: got %v, want ErrUnknownSlug", err)
	}
}

func TestValueForASlugCreatedInTheSameRequestIsAccepted(t *testing.T) {
	svc, _ := svcWithProfileSchema(t, map[string]string{})

	if err := createProfile(t, svc, "mailup-smtp", map[string]string{
		LabelKey("email.profiles", "mailup-smtp"): "MailUp SMTP+",
		"email.profiles.mailup-smtp.host":         "smtp.mailup.it",
	}); err != nil {
		t.Fatalf("an element and its values must be creatable in one PATCH: %v", err)
	}
}
