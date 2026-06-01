package providers

import (
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// BatchRequest, BatchSubmission, BatchResult, BatchStatus, and
// BatchLLMProvider all live in shared/iface so sales can consume them
// without importing this addon. These aliases keep the existing
// implementations in anthropic_batch.go, gemini_batch.go, and
// openai_batch.go working unchanged.
type (
	BatchRequest     = iface.BatchRequest
	BatchSubmission  = iface.BatchSubmission
	BatchResult      = iface.BatchResult
	BatchStatus      = iface.BatchStatus
	BatchLLMProvider = iface.BatchLLMProvider
)
