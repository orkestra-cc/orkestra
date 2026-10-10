package models

import "time"

// LLMGrant lets one user use one model in one org. Managing a model never
// grants its use; only a row here (or Access == everyone) does. The LLM
// prefix keeps the Huma schema name unique across modules.
type LLMGrant struct {
	UUID      string    `bson:"uuid" json:"uuid"`
	TenantID  string    `bson:"tenantId" json:"-"`
	ModelUUID string    `bson:"modelUuid" json:"modelUuid"`
	UserUUID  string    `bson:"userUuid" json:"userUuid"`
	GrantedBy string    `bson:"grantedBy" json:"grantedBy"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
}
