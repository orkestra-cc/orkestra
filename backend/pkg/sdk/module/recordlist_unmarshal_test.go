package module

import "testing"

type profile struct {
	Slug     string `module:"slug"`
	Label    string `module:"label"`
	Host     string `module:"host"`
	Password string `module:"password"`
}

type notificationSettings struct {
	Profiles []profile `module:"email.profiles"`
}

func TestUnmarshalRecordList(t *testing.T) {
	schema := []ConfigField{
		{Key: "email.profiles", Type: FieldRecordList, Items: []ConfigItemField{
			{Key: "host", Type: FieldString},
			{Key: "password", Type: FieldSecret},
		}},
	}
	values := map[string]string{
		"email.profiles.__items":   "a,b",
		"email.profiles.a.__label": "Primary",
		"email.profiles.a.host":    "smtp.a",
		"email.profiles.b.__label": "Backup",
		"email.profiles.b.host":    "smtp.b",
	}

	var out notificationSettings
	if err := UnmarshalConfig(schema, values, nil, &out); err != nil {
		t.Fatalf("UnmarshalConfig: %v", err)
	}
	if len(out.Profiles) != 2 {
		t.Fatalf("got %d profiles, want 2", len(out.Profiles))
	}
	if out.Profiles[0].Slug != "a" || out.Profiles[0].Label != "Primary" || out.Profiles[0].Host != "smtp.a" {
		t.Fatalf("first profile decoded wrong: %+v", out.Profiles[0])
	}
	if out.Profiles[1].Slug != "b" {
		t.Fatalf("roster order not preserved: %+v", out.Profiles)
	}
}

// An empty roster decodes to an empty slice, not an error: a module can
// declare a record list long before an operator populates it.
func TestUnmarshalRecordListWithNoElements(t *testing.T) {
	schema := []ConfigField{
		{Key: "email.profiles", Type: FieldRecordList, Items: []ConfigItemField{
			{Key: "host", Type: FieldString},
		}},
	}
	var out notificationSettings
	if err := UnmarshalConfig(schema, map[string]string{}, nil, &out); err != nil {
		t.Fatalf("UnmarshalConfig: %v", err)
	}
	if len(out.Profiles) != 0 {
		t.Fatalf("empty roster decoded to %d elements", len(out.Profiles))
	}
}

// A sub-field's Default applies per element, exactly as a top-level field's
// does — the element simply has no stored value for it yet.
func TestUnmarshalRecordListAppliesSubFieldDefaults(t *testing.T) {
	type portProfile struct {
		Slug string `module:"slug"`
		Port int    `module:"port"`
	}
	type settings struct {
		Profiles []portProfile `module:"email.profiles"`
	}
	schema := []ConfigField{
		{Key: "email.profiles", Type: FieldRecordList, Items: []ConfigItemField{
			{Key: "port", Type: FieldInt, Default: "587"},
		}},
	}
	var out settings
	if err := UnmarshalConfig(schema, map[string]string{"email.profiles.__items": "a"}, nil, &out); err != nil {
		t.Fatalf("UnmarshalConfig: %v", err)
	}
	if len(out.Profiles) != 1 || out.Profiles[0].Port != 587 {
		t.Fatalf("sub-field default not applied: %+v", out.Profiles)
	}
}
