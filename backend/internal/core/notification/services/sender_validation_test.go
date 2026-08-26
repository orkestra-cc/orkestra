package services

import (
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// profileValues builds a flat map for a roster. Each entry is slug → sub-field values.
func profileValues(order []string, elems map[string]map[string]string) map[string]string {
	v := map[string]string{module.RosterKey(SendersField): module.FormatRoster(order)}
	for slug, subs := range elems {
		for k, val := range subs {
			v[module.ItemKey(SendersField, slug, k)] = val
		}
	}
	return v
}

func validationDrivers() *DriverRegistry { return NewDriverRegistry(CoreDrivers(nil)...) }

func TestValidateSenderConfig_ThreeStates(t *testing.T) {
	smtpOK := map[string]string{SubProvider: "smtp", SubFromAddress: "f@x", SubSMTPHost: "h", SubSMTPPort: "25"}
	cases := []struct {
		name      string
		values    map[string]string
		wantCode  string
		wantField string
	}{
		// State 1 — empty roster: a legacy install; a PATCH touching only app.name must pass.
		{"empty roster passes", map[string]string{"app.name": "Orkestra", "email.provider": "smtp"}, "", ""},
		{"nil map passes", nil, "", ""},
		// State 2 — roster of drafts: the first save of the first profile carries no patterns.
		{"drafts only pass even with an unknown driver",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop"}, "b": {SubProvider: "sendgrid"}}), "", ""},
		// State 3 — routing map.
		{"one default routing profile passes",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*"}}), "", ""},
		{"pattern without a default",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "auth.*"}}),
			errcode.NotificationSenderNoDefault, SendersField},
		{"two defaults",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*"}, "b": {SubProvider: "noop", SubCategories: "*"}}),
			errcode.NotificationSenderDuplicateDefault, module.ItemKey(SendersField, "b", SubCategories)},
		{"same pattern on two profiles",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*, auth.*"}, "b": {SubProvider: "noop", SubCategories: "Auth.*"}}),
			errcode.NotificationSenderPatternConflict, module.ItemKey(SendersField, "b", SubCategories)},
		{"within-profile repeat is not a conflict",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*, auth.*, auth.*, AUTH.*"}}), "", ""},
		{"malformed pattern",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*, auth*"}}),
			errcode.NotificationSenderPatternInvalid, module.ItemKey(SendersField, "a", SubCategories)},
		{"a profile whose only pattern is malformed is not a draft",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*"}, "b": {SubProvider: "noop", SubCategories: "auth.*.google"}}),
			errcode.NotificationSenderPatternInvalid, module.ItemKey(SendersField, "b", SubCategories)},
		{"draft with unknown driver beside a live profile saves",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*"}, "b": {SubProvider: "sendgrid"}}), "", ""},
		{"the same profile once it declares a pattern is rejected",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*"}, "b": {SubProvider: "sendgrid", SubCategories: "crm.*"}}),
			errcode.NotificationSenderUnknownDriver, module.ItemKey(SendersField, "b", SubProvider)},
		{"routing smtp profile missing host",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "smtp", SubCategories: "*", SubFromAddress: "f@x"}}),
			errcode.NotificationSenderIncomplete, module.ItemKey(SendersField, "a", SubSMTPHost)},
		{"anonymous smtp relay is complete", profileValues([]string{"a"}, map[string]map[string]string{"a": mergeSubs(smtpOK, SubCategories, "*")}), "", ""},
		{"routing mailup profile missing user",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "mailup", SubCategories: "*", SubFromAddress: "f@x"}}),
			errcode.NotificationSenderIncomplete, module.ItemKey(SendersField, "a", SubMailUpUser)},
		{"routing mailup profile missing only its secret saves (secret-blind)",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "mailup", SubCategories: "*", SubFromAddress: "f@x", SubMailUpUser: "s1_2"}}), "", ""},
		{"draft smtp profile missing host saves",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*"}, "b": {SubProvider: "smtp"}}), "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateSenderConfig(c.values, validationDrivers())
			if c.wantCode == "" {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			var ve *module.ConfigValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want *ConfigValidationError, got %v", err)
			}
			if ve.Code != c.wantCode || ve.Field != c.wantField {
				t.Fatalf("code=%q field=%q, want %q %q (message %q)", ve.Code, ve.Field, c.wantCode, c.wantField, ve.Message)
			}
		})
	}
}

func mergeSubs(base map[string]string, kv ...string) map[string]string {
	out := make(map[string]string, len(base)+len(kv)/2)
	for k, v := range base {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

// TestValidateSenderConfig_IsSecretBlind documents the D5 limit rather than
// leaving it implicit: a routing profile whose only gap is a secret saves
// cleanly here and is caught by IsConfiguredFor at request time instead.
func TestValidateSenderConfig_IsSecretBlind(t *testing.T) {
	secretDriver := &reqDriver{name: "vendor", reqs: []ProfileRequirement{{Key: SubFromAddress}, {Key: SubSMTPPassword, Secret: true}}}
	values := profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "vendor", SubCategories: "*", SubFromAddress: "f@x"}})
	if err := ValidateSenderConfig(values, NewDriverRegistry(secretDriver)); err != nil {
		t.Fatalf("the save-time gate cannot see secrets and must not reject on one: %v", err)
	}
}
