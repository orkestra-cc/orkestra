package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Permission is the unit of authorization. Modules declare permissions at
// boot via Module.Permissions() and the registry upserts them here. Admins
// bind permissions to roles; users inherit them through role bindings.
type Permission struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	Key         string             `bson:"key" json:"key"`
	Module      string             `bson:"module" json:"module"`
	Description string             `bson:"description" json:"description"`
	System      bool               `bson:"system" json:"system"`
	CreatedAt   time.Time          `bson:"createdAt" json:"createdAt"`
}

// Role is a named bag of permissions. System roles (IsSystem=true) are
// seeded on first boot of the authz module; custom roles are created by
// tenant administrators and scoped to a specific tenantId. System roles
// have an empty tenantId.
type Role struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID        string             `bson:"uuid" json:"id"`
	TenantID    string             `bson:"tenantId" json:"tenantId"` // empty for system roles
	Name        string             `bson:"name" json:"name"`
	Description string             `bson:"description" json:"description"`
	Permissions []string           `bson:"permissions" json:"permissions"`
	IsSystem    bool               `bson:"isSystem" json:"isSystem"`
	IsActive    bool               `bson:"isActive" json:"isActive"`
	CreatedAt   time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// Binding grants a role to a user in a specific tenant (or globally when
// TenantID is empty — used for system role bindings derived from the user's
// system role). Optional ExpiresAt supports contractor/trial grants.
//
// There is NO TTL index on expiresAt (a plain index only) and no background
// reaper — both are tracked as future work in this module's CLAUDE.md — so
// an expired row survives indefinitely. Because the unique
// (tenantId, userUUID, roleId) index would then let a dead grant block
// every future grant of that role, the repository's CreateBinding and
// EnsureBinding reap the tuple's own expired row before re-granting it.
type Binding struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID      string             `bson:"uuid" json:"id"`
	UserUUID  string             `bson:"userUUID" json:"userUUID"`
	TenantID  string             `bson:"tenantId" json:"tenantId"` // empty for system-level grants
	RoleUUID  string             `bson:"roleId" json:"roleId"`
	RoleName  string             `bson:"roleName" json:"roleName"`
	GrantedBy string             `bson:"grantedBy,omitempty" json:"grantedBy,omitempty"`
	GrantedAt time.Time          `bson:"grantedAt" json:"grantedAt"`
	ExpiresAt *time.Time         `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`
}

// --- DTOs ---

type CreateRoleInput struct {
	Name        string   `json:"name" validate:"required,min=1,max=80"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions" validate:"required,min=1"`
}

type UpdateRoleInput struct {
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	IsActive    *bool    `json:"isActive,omitempty"`
}

type CreateBindingInput struct {
	UserUUID  string     `json:"userUUID" validate:"required"`
	RoleUUID  string     `json:"roleId" validate:"required"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type PermissionCatalogResponse struct {
	Permissions []Permission `json:"permissions"`
}

type RoleListResponse struct {
	Roles []Role `json:"roles"`
}

type BindingListResponse struct {
	Bindings []Binding `json:"bindings"`
}

type EffectivePermissionsResponse struct {
	TenantID    string   `json:"tenantId"`
	Permissions []string `json:"permissions"`
	SystemRole  string   `json:"systemRole"`
}
