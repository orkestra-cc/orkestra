package models

import (
	"reflect"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/retentionclass"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestNewPlatformPolicy(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	p := NewPlatformPolicy("p-1", now)
	if p.UUID != "p-1" || !p.IsPlatformDefault || p.Version != 1 || p.Name != PlatformPolicyName {
		t.Fatalf("identity fields: %+v", p)
	}
	if p.CreatedBy != SystemActor || p.UpdatedBy != SystemActor || !p.CreatedAt.Equal(now) || p.ChangeReason == "" {
		t.Fatalf("authorship fields: %+v", p)
	}
	if !reflect.DeepEqual(p.LogContent, iface.DefaultLogContentPolicy()) {
		t.Fatalf("log content = %+v", p.LogContent)
	}
	if !reflect.DeepEqual(p.Retention, retentionclass.DefaultRetention()) {
		t.Fatalf("retention = %v", p.Retention)
	}
	s := p.Sinks
	if s == nil || s.Loki.Days != 14 || s.Loki.WarnErrorDays != 30 || s.Tempo.Days != 3 ||
		s.Prometheus.Days != 15 || s.ContainerLogs.MaxSizeMB != 50 || s.ContainerLogs.MaxFiles != 5 ||
		s.Backups.Days != 30 || s.Backups.MinKeep != 3 || s.DSRExportDays != 30 ||
		s.Spool.DeadLetterDays != 30 || s.External == nil || len(s.External) != 0 {
		t.Fatalf("sinks = %+v", s)
	}
	if p.Accountability.Role != RoleController || !p.Accountability.ReviewDueAt.Equal(now.AddDate(1, 0, 0)) {
		t.Fatalf("accountability = %+v", p.Accountability)
	}
}

func TestPolicyInputIsADeepCopy(t *testing.T) {
	p := NewPlatformPolicy("p-1", time.Now())
	in := p.Input()
	in.LogContent.PIIKeys[0] = "zzz"
	in.Retention[iface.RetentionAdminAccess] = 999
	in.Sinks.External = append(in.Sinks.External, ExternalSink{Name: "x"})
	in.Sinks.Loki.Days = 1
	if p.LogContent.PIIKeys[0] == "zzz" || p.Retention[iface.RetentionAdminAccess] == 999 ||
		len(p.Sinks.External) != 0 || p.Sinks.Loki.Days != 14 {
		t.Fatal("Input shares state with the policy")
	}
}

func TestValidRole(t *testing.T) {
	for _, r := range []string{RoleController, RoleProcessor, RoleJointController} {
		if !ValidRole(r) {
			t.Fatalf("%q rejected", r)
		}
	}
	if ValidRole("owner") || ValidRole("") {
		t.Fatal("unknown role accepted")
	}
}
