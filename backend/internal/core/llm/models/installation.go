package models

import "time"

// InstallationDocID is the _id of the single llm_installation document.
const InstallationDocID = "siwc"

// Installation holds the per-installation ext_agent_host_id OpenAI requires
// ("distinct for each host"). Global, not per org: one Mongo deployment is
// one host. The only module collection without tenantId.
type Installation struct {
	ID             string    `bson:"_id"`
	ExtAgentHostID string    `bson:"extAgentHostId"`
	CreatedAt      time.Time `bson:"createdAt"`
}
