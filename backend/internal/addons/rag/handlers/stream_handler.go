package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra/backend/internal/addons/rag/services"
)

// StreamHandler handles SSE streaming for RAG queries
type StreamHandler struct {
	queryService services.QueryService
	logger       *slog.Logger
}

// NewStreamHandler creates a new StreamHandler
func NewStreamHandler(querySvc services.QueryService, logger *slog.Logger) *StreamHandler {
	return &StreamHandler{
		queryService: querySvc,
		logger:       logger.With(slog.String("handler", "rag-stream")),
	}
}

// streamRequest mirrors RAGQueryRequest.Body for manual JSON parsing
type streamRequest struct {
	Question         string  `json:"question"`
	TopK             int     `json:"topK"`
	MinScore         float64 `json:"minScore"`
	ISOStandard      string  `json:"isoStandard"`
	ModelUUID        string  `json:"modelUuid"`
	RequirementLevel string  `json:"requirementLevel"`
	NodeType         string  `json:"nodeType"`
	RetrievalMode    string  `json:"retrievalMode"`
}

// sourcesEvent is sent immediately after vector search completes
type sourcesEvent struct {
	Sources  []iface.SourceRef `json:"sources"`
	Metadata iface.QueryMeta   `json:"metadata"`
}

// tokenEvent is sent for each LLM token
type tokenEvent struct {
	Text string `json:"text"`
}

// doneEvent is sent when streaming is complete
type doneEvent struct {
	Metadata iface.QueryMeta `json:"metadata"`
}

// errorEvent is sent when an error occurs
type errorEvent struct {
	Error string `json:"error"`
}

// HandleQueryStream handles POST /v1/rag/query/stream as SSE
func (h *StreamHandler) HandleQueryStream(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req streamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Question == "" {
		http.Error(w, `{"error":"question is required"}`, http.StatusBadRequest)
		return
	}

	// Run query preparation + start streaming
	result, err := h.queryService.QueryStream(r.Context(), req.Question, req.TopK, req.MinScore, req.ISOStandard, req.ModelUUID, req.RequirementLevel, req.NodeType, req.RetrievalMode, nil)
	if err != nil {
		h.logger.Error("QueryStream failed", slog.String("error", err.Error()))
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer result.Cancel()

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx/proxy buffering

	// Use ResponseController for flushing (works through wrapped writers)
	rc := http.NewResponseController(w)

	// Send sources event immediately
	h.writeSSE(w, rc, "sources", sourcesEvent{
		Sources:  result.Sources,
		Metadata: result.PreMeta,
	})

	// If no sources found, send done immediately with canned answer
	if result.TokenChan == nil {
		h.writeSSE(w, rc, "answer", tokenEvent{
			Text: "No relevant information found in the knowledge base for your question.",
		})
		h.writeSSE(w, rc, "done", doneEvent{Metadata: result.PreMeta})
		return
	}

	// Stream tokens from LLM with keepalive during prefill
	clientGone := r.Context().Done()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-clientGone:
			h.logger.Info("Client disconnected during streaming")
			return
		case <-keepalive.C:
			// SSE comment line keeps the connection alive through proxies during LLM prefill
			fmt.Fprintf(w, ": keepalive\n\n")
			if err := rc.Flush(); err != nil {
				return
			}
		case chunk, ok := <-result.TokenChan:
			if !ok {
				// Channel closed unexpectedly
				h.writeSSE(w, rc, "done", doneEvent{
					Metadata: h.buildFinalMeta(result),
				})
				return
			}
			if chunk.Error != nil {
				h.logger.Error("LLM stream error", slog.String("error", chunk.Error.Error()))
				h.writeSSE(w, rc, "error", errorEvent{Error: chunk.Error.Error()})
				return
			}
			if chunk.Done {
				h.writeSSE(w, rc, "done", doneEvent{
					Metadata: h.buildFinalMeta(result),
				})
				h.logger.Info("RAG stream completed",
					slog.Int64("totalMs", time.Since(result.StartTime).Milliseconds()),
					slog.Int("chunks", len(result.Sources)),
				)
				return
			}
			if chunk.Text != "" {
				h.writeSSE(w, rc, "token", tokenEvent{Text: chunk.Text})
			}
		}
	}
}

func (h *StreamHandler) buildFinalMeta(result *services.StreamResult) iface.QueryMeta {
	return iface.QueryMeta{
		EmbeddingTimeMs: result.PreMeta.EmbeddingTimeMs,
		SearchTimeMs:    result.PreMeta.SearchTimeMs,
		LLMTimeMs:       time.Since(result.LLMStart).Milliseconds(),
		TotalTimeMs:     time.Since(result.StartTime).Milliseconds(),
		ChunksRetrieved: result.PreMeta.ChunksRetrieved,
		ModelUsed:       result.PreMeta.ModelUsed,
	}
}

func (h *StreamHandler) writeSSE(w http.ResponseWriter, rc *http.ResponseController, event string, data interface{}) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		h.logger.Error("Failed to marshal SSE data", slog.String("error", err.Error()))
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonBytes)
	if err := rc.Flush(); err != nil {
		h.logger.Error("Failed to flush SSE data", slog.String("error", err.Error()))
	}
}
