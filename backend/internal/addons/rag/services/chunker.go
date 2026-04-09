package services

import (
	"strings"
	"unicode"
)

// ChunkStructured walks the structural tree and produces chunks that respect
// structural boundaries. Each chunk inherits rich metadata from the tree.
//
// maxSize: maximum chunk size in characters (default 1024)
// minSize: minimum chunk size before merging with siblings (default 128)
func ChunkStructured(root *StructuralNode, maxSize, minSize int) []StructuredChunk {
	if maxSize <= 0 {
		maxSize = 1024
	}
	if minSize <= 0 {
		minSize = 128
	}

	var chunks []StructuredChunk
	position := 0

	// Recursive function to process each node
	var processNode func(node *StructuralNode)
	processNode = func(node *StructuralNode) {
		if node.NodeType == "document" {
			for _, child := range node.Children {
				processNode(child)
			}
			return
		}

		// Leaf node (no children): emit as chunk(s)
		if len(node.Children) == 0 {
			text := buildNodeText(node)
			if strings.TrimSpace(text) == "" {
				return
			}

			fullPath := BuildFullPath(node)
			sectionUUID := findParentSectionUUID(node)

			if len(text) <= maxSize {
				// Fits in one chunk
				chunks = append(chunks, StructuredChunk{
					Text:             text,
					Position:         position,
					NodeType:         effectiveNodeType(node),
					Numbering:        node.Numbering,
					FullPath:         fullPath,
					RequirementLevel: node.RequirementLevel,
					Depth:            node.Depth,
					SectionUUID:      sectionUUID,
				})
				position++
			} else {
				// Split at sentence boundaries
				parts := splitAtSentences(text, maxSize)
				for _, part := range parts {
					chunks = append(chunks, StructuredChunk{
						Text:             part,
						Position:         position,
						NodeType:         effectiveNodeType(node),
						Numbering:        node.Numbering,
						FullPath:         fullPath,
						RequirementLevel: DetectRequirementLevel(part),
						Depth:            node.Depth,
						SectionUUID:      sectionUUID,
					})
					position++
				}
			}
			return
		}

		// If a node has introductory text and all children are list items or notes,
		// try to keep them together as a single chunk (e.g., "The organization shall: a) ... b) ...").
		if allChildrenAreLeafItems(node) {
			combined := buildNodeWithChildrenText(node)
			if strings.TrimSpace(combined) != "" && len(combined) <= maxSize {
				fullPath := BuildFullPath(node)
				chunks = append(chunks, StructuredChunk{
					Text:             combined,
					Position:         position,
					NodeType:         effectiveNodeType(node),
					Numbering:        node.Numbering,
					FullPath:         fullPath,
					RequirementLevel: DetectRequirementLevel(combined),
					Depth:            node.Depth,
					SectionUUID:      node.UUID,
				})
				position++
				return
			}
		}

		// Non-leaf node: emit own text (if any) as chunk, then process children
		if text := strings.TrimSpace(node.Text); text != "" {
			fullPath := BuildFullPath(node)
			sectionUUID := node.UUID // this node IS the section

			if len(text) <= maxSize {
				chunks = append(chunks, StructuredChunk{
					Text:             text,
					Position:         position,
					NodeType:         effectiveNodeType(node),
					Numbering:        node.Numbering,
					FullPath:         fullPath,
					RequirementLevel: node.RequirementLevel,
					Depth:            node.Depth,
					SectionUUID:      sectionUUID,
				})
				position++
			} else {
				parts := splitAtSentences(text, maxSize)
				for _, part := range parts {
					chunks = append(chunks, StructuredChunk{
						Text:             part,
						Position:         position,
						NodeType:         effectiveNodeType(node),
						Numbering:        node.Numbering,
						FullPath:         fullPath,
						RequirementLevel: DetectRequirementLevel(part),
						Depth:            node.Depth,
						SectionUUID:      sectionUUID,
					})
					position++
				}
			}
		}

		// Merge small children before processing
		merged := mergeSmallSiblings(node.Children, minSize, maxSize)
		for _, group := range merged {
			if len(group) == 1 {
				processNode(group[0])
			} else {
				// Merged group: combine their text into one chunk
				var texts []string
				for _, child := range group {
					t := buildNodeText(child)
					if strings.TrimSpace(t) != "" {
						texts = append(texts, t)
					}
				}
				combined := strings.Join(texts, "\n\n")
				if strings.TrimSpace(combined) == "" {
					continue
				}
				// Use the first child's metadata
				first := group[0]
				fullPath := BuildFullPath(first)
				sectionUUID := findParentSectionUUID(first)

				chunks = append(chunks, StructuredChunk{
					Text:             combined,
					Position:         position,
					NodeType:         effectiveNodeType(first),
					Numbering:        first.Numbering,
					FullPath:         fullPath,
					RequirementLevel: DetectRequirementLevel(combined),
					Depth:            first.Depth,
					SectionUUID:      sectionUUID,
				})
				position++
			}
		}
	}

	processNode(root)

	return chunks
}

// buildNodeText constructs the display text for a leaf node, including title as header.
func buildNodeText(node *StructuralNode) string {
	var sb strings.Builder

	// Add a heading line from numbering + title
	heading := ""
	if node.Numbering != "" && node.Title != "" {
		heading = node.Numbering + " " + node.Title
	} else if node.Title != "" {
		heading = node.Title
	} else if node.Numbering != "" {
		heading = node.Numbering
	}

	if heading != "" {
		sb.WriteString(heading)
		if node.Text != "" {
			sb.WriteString("\n\n")
		}
	}
	if node.Text != "" {
		sb.WriteString(node.Text)
	}

	return sb.String()
}

// effectiveNodeType returns the node type, classifying nodes with requirement
// language as "requirement" if they're generic clause/subclause types.
func effectiveNodeType(node *StructuralNode) string {
	if node.RequirementLevel != "" &&
		(node.NodeType == "clause" || node.NodeType == "subclause" || node.NodeType == "article" || node.NodeType == "paragraph" || node.NodeType == "list_item") {
		return "requirement"
	}
	return node.NodeType
}

// findParentSectionUUID walks up the tree to find the nearest section-level parent's UUID.
func findParentSectionUUID(node *StructuralNode) string {
	n := node.Parent
	for n != nil && n.NodeType != "document" {
		return n.UUID
	}
	if node.Parent != nil {
		return node.Parent.UUID
	}
	return ""
}

// mergeSmallSiblings groups adjacent small leaf siblings so they can be merged
// into a single chunk. Returns groups — a group of 1 means process normally,
// a group of 2+ means merge into one chunk.
func mergeSmallSiblings(children []*StructuralNode, minSize, maxSize int) [][]*StructuralNode {
	if len(children) == 0 {
		return nil
	}

	var groups [][]*StructuralNode
	var currentGroup []*StructuralNode
	currentSize := 0

	for _, child := range children {
		// Non-leaf children are never merged
		if len(child.Children) > 0 {
			if len(currentGroup) > 0 {
				groups = append(groups, currentGroup)
				currentGroup = nil
				currentSize = 0
			}
			groups = append(groups, []*StructuralNode{child})
			continue
		}

		childSize := len(buildNodeText(child))

		// Large enough on its own — don't merge
		if childSize >= minSize {
			if len(currentGroup) > 0 {
				groups = append(groups, currentGroup)
				currentGroup = nil
				currentSize = 0
			}
			groups = append(groups, []*StructuralNode{child})
			continue
		}

		// Small child — try to merge with current group
		if currentSize+childSize+2 > maxSize {
			// Would exceed max — flush current group and start new one
			if len(currentGroup) > 0 {
				groups = append(groups, currentGroup)
			}
			currentGroup = []*StructuralNode{child}
			currentSize = childSize
		} else {
			currentGroup = append(currentGroup, child)
			currentSize += childSize + 2 // +2 for \n\n separator
		}
	}

	if len(currentGroup) > 0 {
		groups = append(groups, currentGroup)
	}

	return groups
}

// splitAtSentences splits text into parts of at most maxSize characters,
// breaking at sentence boundaries (". " or ".\n").
func splitAtSentences(text string, maxSize int) []string {
	if len(text) <= maxSize {
		return []string{text}
	}

	var parts []string
	remaining := text

	for len(remaining) > maxSize {
		// Find the last sentence boundary within maxSize
		cutPoint := findSentenceBoundary(remaining, maxSize)
		if cutPoint <= 0 {
			// No sentence boundary found — force cut at maxSize
			cutPoint = maxSize
			// Try to at least cut at a word boundary
			for cutPoint > maxSize/2 {
				if remaining[cutPoint] == ' ' || remaining[cutPoint] == '\n' {
					break
				}
				cutPoint--
			}
			if cutPoint <= maxSize/2 {
				cutPoint = maxSize
			}
		}

		parts = append(parts, strings.TrimSpace(remaining[:cutPoint]))
		remaining = strings.TrimSpace(remaining[cutPoint:])
	}

	if len(remaining) > 0 {
		parts = append(parts, remaining)
	}

	return parts
}

// findSentenceBoundary finds the last position within maxLen where a sentence ends.
// Returns -1 if no sentence boundary is found.
func findSentenceBoundary(text string, maxLen int) int {
	if maxLen > len(text) {
		maxLen = len(text)
	}

	lastBoundary := -1
	runes := []rune(text[:maxLen])

	for i := 0; i < len(runes)-1; i++ {
		if runes[i] == '.' || runes[i] == '?' || runes[i] == '!' {
			next := runes[i+1]
			if next == ' ' || next == '\n' || next == '\r' || unicode.IsUpper(next) {
				lastBoundary = i + 1
			}
		}
	}

	return lastBoundary
}

// allChildrenAreLeafItems returns true if a node has children and every child
// is a leaf (no grandchildren) with a type like list_item, note, or example.
func allChildrenAreLeafItems(node *StructuralNode) bool {
	if len(node.Children) == 0 {
		return false
	}
	for _, child := range node.Children {
		if len(child.Children) > 0 {
			return false
		}
		switch child.NodeType {
		case "list_item", "note", "example":
			// ok
		default:
			return false
		}
	}
	return true
}

// buildNodeWithChildrenText combines a node's own text with all its children's
// text to produce a single combined string (parent intro + list items).
func buildNodeWithChildrenText(node *StructuralNode) string {
	var sb strings.Builder
	heading := buildNodeText(node)
	if heading != "" {
		sb.WriteString(heading)
	}
	for _, child := range node.Children {
		childText := buildNodeText(child)
		if childText != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(childText)
		}
	}
	return sb.String()
}
