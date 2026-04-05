package reporting

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/reporting/handlers"
)

// RegisterRoutes registers all reporting routes on the given API.
func RegisterRoutes(api huma.API, deadlineHandler *handlers.DeadlineHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "get-deadline-report",
		Method:      http.MethodGet,
		Path:        "/v1/reports/deadlines",
		Summary:     "Get deadline report",
		Description: "Retrieves all items with expiry dates (users, certifications)",
		Tags:        []string{"Reports"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, deadlineHandler.GetDeadlines)
}
