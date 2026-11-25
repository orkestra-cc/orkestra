package repository

import (
	"go.mongodb.org/mongo-driver/bson"

	"github.com/orkestra/backend/internal/core/llm/models"
)

// Per-subject selectors of the DSR pipeline. The export, the erasure and the
// dry-run count of a collection all build their filter here, so a purge and
// the preview of that purge cannot drift apart.

// userUUIDFilter: llm_grants, llm_usage and llm_user_accounts rows keyed on the
// subject's userUuid.
func userUUIDFilter(userUUID string) bson.M { return bson.M{"userUuid": userUUID} }

// finalizedByUserFilter: the subject's usage rows a hard delete removes (the
// pending ones, whose reservation is still in the rollup, are anonymized
// instead). CountByUser counts userUUIDFilter, the union of both.
func finalizedByUserFilter(userUUID string) bson.M {
	return bson.M{"userUuid": userUUID, "outcome": bson.M{"$ne": models.OutcomePending}}
}

// createdByFilter: llm_credentials and llm_models created by the subject.
func createdByFilter(userUUID string) bson.M { return bson.M{"createdBy": userUUID} }

// grantedByFilter: grants the subject handed out (any grantee).
func grantedByFilter(userUUID string) bson.M { return bson.M{"grantedBy": userUUID} }

// issuedToOthersFilter: grants the subject handed out to someone else. The
// purge deletes the subject's own grants first (a self-grant is one deleted
// row), so only these are left for the granter rewrite.
func issuedToOthersFilter(userUUID string) bson.M {
	return bson.M{"grantedBy": userUUID, "userUuid": bson.M{"$ne": userUUID}}
}

// budgetRecipientFilter: budgets listing the subject as an extra alert recipient.
func budgetRecipientFilter(userUUID string) bson.M {
	return bson.M{"notifyExtraUserUuids": userUUID}
}

// budgetUpdaterFilter: budgets last edited by the subject.
func budgetUpdaterFilter(userUUID string) bson.M { return bson.M{"updatedBy": userUUID} }

// deliveredToFilter: budget alerts delivered to the subject.
func deliveredToFilter(userUUID string) bson.M { return bson.M{"deliveredTo": userUUID} }
