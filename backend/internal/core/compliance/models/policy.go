package models

import (
	"maps"
	"slices"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/retentionclass"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Policy-engine collections (compliance spec §1.1). Platform state managed
// by Tier-1 operators: queries on them are not tenant-scoped except the
// assignment lookups keyed by tenantId.
const (
	PoliciesCollection                = "compliance_policies"
	PolicyVersionsCollection          = "compliance_policy_versions"
	PolicyAssignmentsCollection       = "compliance_policy_assignments"
	PolicyAssignmentHistoryCollection = "compliance_policy_assignment_history"
	PolicyChangeRequestsCollection    = "compliance_policy_change_requests"
)

// SystemActor authors the platform policy created at first boot.
const SystemActor = "system"

// PlatformPolicyName is the name of the platform policy created at first boot.
const PlatformPolicyName = "Platform"

// Accountability roles (GDPR art. 4, 26, 28).
const (
	RoleController      = "controller"
	RoleProcessor       = "processor"
	RoleJointController = "joint_controller"
)

func ValidRole(r string) bool {
	return r == RoleController || r == RoleProcessor || r == RoleJointController
}

// Accountability carries the GDPR art. 5(2)/30 metadata of a policy (spec
// D13). Owner is an operator UUID: personal data kept as compliance evidence.
type Accountability struct {
	Role          string    `bson:"role" json:"role"`
	RoPARef       string    `bson:"ropaRef" json:"ropaRef" maxLength:"200"`
	AssessmentRef string    `bson:"assessmentRef" json:"assessmentRef" maxLength:"200"`
	Owner         string    `bson:"owner" json:"owner" maxLength:"64"`
	ReviewDueAt   time.Time `bson:"reviewDueAt" json:"reviewDueAt,omitzero" required:"false"`
}

// External sink kinds (spec §5.1).
const (
	ExternalKindOTLP          = "otlp"
	ExternalKindSyslog        = "syslog"
	ExternalKindBackupOffsite = "backup_offsite"
	ExternalKindOther         = "other"
)

type LokiSink struct {
	Days          int `bson:"days" json:"days"`
	WarnErrorDays int `bson:"warnErrorDays" json:"warnErrorDays"`
}

type DaysSink struct {
	Days int `bson:"days" json:"days"`
}

type ContainerLogsSink struct {
	MaxSizeMB int `bson:"maxSizeMb" json:"maxSizeMb"`
	MaxFiles  int `bson:"maxFiles" json:"maxFiles"`
}

type BackupsSink struct {
	Days    int `bson:"days" json:"days"`
	MinKeep int `bson:"minKeep" json:"minKeep"`
}

type SpoolSink struct {
	DeadLetterDays int `bson:"deadLetterDays" json:"deadLetterDays"`
}

// ExternalSink is a processor-held copy of the logs the platform can only
// declare and review (spec §5.1).
type ExternalSink struct {
	Name                  string    `bson:"name" json:"name" maxLength:"80"`
	Kind                  string    `bson:"kind" json:"kind"`
	Processor             string    `bson:"processor" json:"processor" maxLength:"200"`
	DPARef                string    `bson:"dpaRef" json:"dpaRef" maxLength:"200"`
	Region                string    `bson:"region" json:"region" maxLength:"80"`
	TransferBasis         string    `bson:"transferBasis" json:"transferBasis" maxLength:"200"`
	DeclaredRetentionDays int       `bson:"declaredRetentionDays" json:"declaredRetentionDays"`
	ReviewDueAt           time.Time `bson:"reviewDueAt" json:"reviewDueAt,omitzero" required:"false"`
}

// SinkPolicy is the platform-only retention of every log sink (spec §5.1).
// The T2 stores and validates it; T5 applies it.
type SinkPolicy struct {
	Loki          LokiSink          `bson:"loki" json:"loki"`
	Tempo         DaysSink          `bson:"tempo" json:"tempo"`
	Prometheus    DaysSink          `bson:"prometheus" json:"prometheus"`
	ContainerLogs ContainerLogsSink `bson:"containerLogs" json:"containerLogs"`
	Backups       BackupsSink       `bson:"backups" json:"backups"`
	DSRExportDays int               `bson:"dsrExportDays" json:"dsrExportDays"`
	Spool         SpoolSink         `bson:"spool" json:"spool"`
	External      []ExternalSink    `bson:"external" json:"external"`
}

// DefaultSinkPolicy returns the spec §5.1 defaults.
func DefaultSinkPolicy() *SinkPolicy {
	return &SinkPolicy{
		Loki:          LokiSink{Days: 14, WarnErrorDays: 30},
		Tempo:         DaysSink{Days: 3},
		Prometheus:    DaysSink{Days: 15},
		ContainerLogs: ContainerLogsSink{MaxSizeMB: 50, MaxFiles: 5},
		Backups:       BackupsSink{Days: 30, MinKeep: 3},
		DSRExportDays: 30,
		Spool:         SpoolSink{DeadLetterDays: 30},
		External:      []ExternalSink{},
	}
}

func (s *SinkPolicy) Clone() *SinkPolicy {
	if s == nil {
		return nil
	}
	c := *s
	c.External = slices.Clone(s.External)
	if c.External == nil {
		c.External = []ExternalSink{}
	}
	return &c
}

// PolicyInput is the editable part of a policy: what an operator sends and
// what a change request carries.
type PolicyInput struct {
	Name           string                       `bson:"name" json:"name"`
	Description    string                       `bson:"description" json:"description" maxLength:"500"`
	LogContent     iface.LogContentPolicy       `bson:"logContent" json:"logContent"`
	Retention      map[iface.RetentionClass]int `bson:"retention" json:"retention"`
	Sinks          *SinkPolicy                  `bson:"sinks,omitempty" json:"sinks,omitempty"`
	Accountability Accountability               `bson:"accountability" json:"accountability"`
}

// Policy is the current state of a named, versioned compliance policy (spec
// §1.1). Exactly one has IsPlatformDefault; a tenant policy carries only the
// tenant-settable retention classes and no Sinks.
type Policy struct {
	UUID              string                       `bson:"uuid" json:"uuid"`
	Name              string                       `bson:"name" json:"name"`
	Description       string                       `bson:"description" json:"description"`
	IsPlatformDefault bool                         `bson:"isPlatformDefault" json:"isPlatformDefault"`
	Version           int                          `bson:"version" json:"version"`
	LogContent        iface.LogContentPolicy       `bson:"logContent" json:"logContent"`
	Retention         map[iface.RetentionClass]int `bson:"retention" json:"retention"`
	Sinks             *SinkPolicy                  `bson:"sinks,omitempty" json:"sinks,omitempty"`
	Accountability    Accountability               `bson:"accountability" json:"accountability"`
	CreatedBy         string                       `bson:"createdBy" json:"createdBy"`
	UpdatedBy         string                       `bson:"updatedBy" json:"updatedBy"`
	CreatedAt         time.Time                    `bson:"createdAt" json:"createdAt"`
	UpdatedAt         time.Time                    `bson:"updatedAt" json:"updatedAt"`
	ChangeReason      string                       `bson:"changeReason" json:"changeReason"`
}

// Input returns a deep copy of the editable fields.
func (p Policy) Input() PolicyInput {
	lc := p.LogContent
	lc.PIIKeys = slices.Clone(lc.PIIKeys)
	return PolicyInput{
		Name:           p.Name,
		Description:    p.Description,
		LogContent:     lc,
		Retention:      maps.Clone(p.Retention),
		Sinks:          p.Sinks.Clone(),
		Accountability: p.Accountability,
	}
}

// NewPlatformPolicy is the platform policy created at first boot (spec
// §1.2, §10.1): the static log defaults, every class at its default, the
// §5.1 sinks, role controller and a review due in one year.
func NewPlatformPolicy(uuid string, now time.Time) Policy {
	return Policy{
		UUID:              uuid,
		Name:              PlatformPolicyName,
		Description:       "Platform default compliance policy.",
		IsPlatformDefault: true,
		Version:           1,
		LogContent:        iface.DefaultLogContentPolicy(),
		Retention:         retentionclass.DefaultRetention(),
		Sinks:             DefaultSinkPolicy(),
		Accountability:    Accountability{Role: RoleController, ReviewDueAt: now.AddDate(1, 0, 0)},
		CreatedBy:         SystemActor,
		UpdatedBy:         SystemActor,
		CreatedAt:         now,
		UpdatedAt:         now,
		ChangeReason:      "Created at first boot with the platform defaults.",
	}
}

// Version change kinds.
const (
	VersionCreate = "create"
	VersionUpdate = "update"
	VersionDelete = "delete"
)

// PolicyVersion is the immutable copy of one policy version (compliance
// evidence). A deletion writes one more version with ChangeKind "delete".
type PolicyVersion struct {
	PolicyUUID        string    `bson:"policyUuid" json:"policyUuid"`
	Version           int       `bson:"version" json:"version"`
	ChangeKind        string    `bson:"changeKind" json:"changeKind"`
	Snapshot          Policy    `bson:"snapshot" json:"snapshot"`
	ChangedBy         string    `bson:"changedBy" json:"changedBy"`
	ChangedAt         time.Time `bson:"changedAt" json:"changedAt"`
	Reason            string    `bson:"reason" json:"reason"`
	ChangeRequestUUID string    `bson:"changeRequestUuid,omitempty" json:"changeRequestUuid,omitempty"`
	ApprovedBy        string    `bson:"approvedBy,omitempty" json:"approvedBy,omitempty"`
}

// PolicyAssignment links a tenant to a policy (one per tenant).
type PolicyAssignment struct {
	TenantID   string    `bson:"tenantId" json:"tenantId"`
	TenantKind string    `bson:"tenantKind" json:"tenantKind"`
	PolicyUUID string    `bson:"policyUuid" json:"policyUuid"`
	AssignedBy string    `bson:"assignedBy" json:"assignedBy"`
	AssignedAt time.Time `bson:"assignedAt" json:"assignedAt"`
	Reason     string    `bson:"reason" json:"reason"`
}

const (
	AssignmentActionAssign   = "assign"
	AssignmentActionUnassign = "unassign"
)

// PolicyAssignmentHistory is one row per assignment, change or removal.
type PolicyAssignmentHistory struct {
	UUID               string    `bson:"uuid" json:"uuid"`
	TenantID           string    `bson:"tenantId" json:"tenantId"`
	TenantKind         string    `bson:"tenantKind" json:"tenantKind"`
	Action             string    `bson:"action" json:"action"`
	PolicyUUID         string    `bson:"policyUuid,omitempty" json:"policyUuid,omitempty"`
	PreviousPolicyUUID string    `bson:"previousPolicyUuid,omitempty" json:"previousPolicyUuid,omitempty"`
	ChangedBy          string    `bson:"changedBy" json:"changedBy"`
	ChangedAt          time.Time `bson:"changedAt" json:"changedAt"`
	Reason             string    `bson:"reason" json:"reason"`
	ChangeRequestUUID  string    `bson:"changeRequestUuid,omitempty" json:"changeRequestUuid,omitempty"`
	ApprovedBy         string    `bson:"approvedBy,omitempty" json:"approvedBy,omitempty"`
}

// Change request kinds (spec §1.5; shorten_existing arrives with T4).
const (
	ChangeCreate   = "create"
	ChangeUpdate   = "update"
	ChangeAssign   = "assign"
	ChangeUnassign = "unassign"
)

// Change request statuses.
const (
	ChangeStatusPending    = "pending"
	ChangeStatusApproved   = "approved"
	ChangeStatusRejected   = "rejected"
	ChangeStatusSuperseded = "superseded"
	ChangeStatusExpired    = "expired"
)

// ChangePayload is the change a request asks for, typed per kind: Policy for
// create/update, TenantID + PolicyUUID for assign, TenantID for unassign.
// PreviousPolicyUUID is the tenant's assignment when the request was made:
// an approval finding a different one supersedes the request.
type ChangePayload struct {
	Policy             *PolicyInput `bson:"policy,omitempty" json:"policy,omitempty"`
	TenantID           string       `bson:"tenantId,omitempty" json:"tenantId,omitempty"`
	PolicyUUID         string       `bson:"policyUuid,omitempty" json:"policyUuid,omitempty"`
	PreviousPolicyUUID string       `bson:"previousPolicyUuid,omitempty" json:"previousPolicyUuid,omitempty"`
}

// PolicyChangeRequest is a change with warnings waiting for a second
// operator (four eyes, spec §1.5).
type PolicyChangeRequest struct {
	UUID            string        `bson:"uuid" json:"uuid"`
	Kind            string        `bson:"kind" json:"kind"`
	PolicyUUID      string        `bson:"policyUuid,omitempty" json:"policyUuid,omitempty"`
	TenantID        string        `bson:"tenantId,omitempty" json:"tenantId,omitempty"`
	Payload         ChangePayload `bson:"payload" json:"payload"`
	ExpectedVersion int           `bson:"expectedVersion" json:"expectedVersion"`
	Warnings        []string      `bson:"warnings" json:"warnings"`
	Reason          string        `bson:"reason" json:"reason"`
	RequestedBy     string        `bson:"requestedBy" json:"requestedBy"`
	RequestedAt     time.Time     `bson:"requestedAt" json:"requestedAt"`
	Status          string        `bson:"status" json:"status"`
	DecidedBy       string        `bson:"decidedBy,omitempty" json:"decidedBy,omitempty"`
	DecidedAt       *time.Time    `bson:"decidedAt,omitempty" json:"decidedAt,omitempty"`
	DecisionNote    string        `bson:"decisionNote,omitempty" json:"decisionNote,omitempty"`
}
