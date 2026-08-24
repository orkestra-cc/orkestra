package module

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRecordListFieldMarshalsItsItemSchema(t *testing.T) {
	f := ConfigField{
		Key:   "email.profiles",
		Label: "Delivery profiles",
		Type:  FieldRecordList,
		Items: []ConfigItemField{
			{Key: "host", Label: "SMTP host", Type: FieldString, Required: true},
			{Key: "password", Label: "Password", Type: FieldSecret},
		},
	}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back ConfigField
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Items) != 2 || back.Items[1].Type != FieldSecret {
		t.Fatalf("item schema did not round-trip: %+v", back.Items)
	}
}

func TestConfigFieldWithoutItemsOmitsThem(t *testing.T) {
	raw, err := json.Marshal(ConfigField{Key: "k", Type: FieldString})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(raw); strings.Contains(got, "items") {
		t.Fatalf("scalar field emitted an items key: %s", got)
	}
}
