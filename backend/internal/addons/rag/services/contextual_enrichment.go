package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	aimodelsProviders "github.com/orkestra/backend/internal/addons/aimodels/providers"
)

const contextSystemPrompt = "You generate brief contextual descriptions for document chunks. Be concise and factual. Respond with ONLY the context, no preamble. /no_think"

// GenerateChunkContexts calls the LLM to produce a short context prefix for
// each chunk, situating it within the document. This implements Anthropic's
// "Contextual Retrieval" approach: the context is prepended to the chunk text
// before embedding so the vector captures broader document context, while the
// original chunk text is stored separately for display.
//
// If the LLM call fails for any chunk, that chunk gets an empty context and
// will be embedded with its raw text (graceful degradation).
func GenerateChunkContexts(
	ctx context.Context,
	llmProvider aimodelsProviders.LLMProvider,
	docTitle string,
	isoStandard string,
	outline string,
	chunks []StructuredChunk,
	logger *slog.Logger,
) []string {
	contexts := make([]string, len(chunks))

	logger.Info("generating contextual prefixes",
		slog.Int("chunks", len(chunks)),
		slog.String("model", llmProvider.ModelName()),
	)

	for i, chunk := range chunks {
		prompt := fmt.Sprintf(`<document>
Title: %s
Standard: %s
Structure:
%s
</document>

<chunk>
Section: %s
%s
</chunk>

Give a short (2-3 sentence) context that situates this chunk within the document. Include the section number, topic, and how it relates to the overall standard.`, docTitle, isoStandard, outline, chunk.FullPath, chunk.Text)

		result, err := llmProvider.Complete(ctx, prompt, aimodelsProviders.CompletionOptions{
			Temperature:  0.0,
			MaxTokens:    150,
			SystemPrompt: contextSystemPrompt,
		})
		if err != nil {
			logger.Warn("context generation failed for chunk",
				slog.Int("position", i),
				slog.String("error", err.Error()),
			)
			continue // contexts[i] stays ""
		}
		contexts[i] = strings.TrimSpace(result)
	}

	generated := 0
	for _, c := range contexts {
		if c != "" {
			generated++
		}
	}
	logger.Info("contextual prefixes complete",
		slog.Int("generated", generated),
		slog.Int("total", len(chunks)),
	)

	return contexts
}

// BuildDocumentOutline creates a text outline from the structural tree,
// showing the section hierarchy for use in context generation prompts.
func BuildDocumentOutline(root *StructuralNode) string {
	var sb strings.Builder
	buildOutlineRecursive(root, &sb)
	return sb.String()
}

func buildOutlineRecursive(node *StructuralNode, sb *strings.Builder) {
	if node.NodeType != "document" {
		indent := strings.Repeat("  ", node.Depth-1)
		label := ""
		if node.Numbering != "" && node.Title != "" {
			label = node.Numbering + " " + node.Title
		} else if node.Title != "" {
			label = node.Title
		} else if node.Numbering != "" {
			label = node.Numbering
		}
		if label != "" {
			fmt.Fprintf(sb, "%s- %s\n", indent, label)
		}
	}
	for _, child := range node.Children {
		// Only include section-level nodes in the outline (not list items, notes, etc.)
		if child.NodeType == "clause" || child.NodeType == "subclause" ||
			child.NodeType == "article" || child.NodeType == "annex" ||
			child.NodeType == "section" {
			buildOutlineRecursive(child, sb)
		}
	}
}
