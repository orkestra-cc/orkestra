package services

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"

	gmext "github.com/yuin/goldmark/extension"
)

// ParseMarkdownStructure parses raw markdown bytes into a hierarchical
// StructuralNode tree using goldmark's CommonMark AST. The returned tree is
// compatible with the same chunking/embedding pipeline used for ISO documents.
func ParseMarkdownStructure(source []byte) *StructuralNode {
	root := &StructuralNode{
		UUID:     uuid.New().String(),
		NodeType: "document",
		Title:    "Document",
		Depth:    0,
	}

	if len(strings.TrimSpace(string(source))) == 0 {
		return root
	}

	// Clean boilerplate before parsing (copyright, licensing, page headers, etc.)
	source = cleanMarkdownSource(source)

	// Parse with GFM extensions (tables, strikethrough, etc.)
	md := goldmark.New(goldmark.WithExtensions(gmext.Table))
	reader := text.NewReader(source)
	doc := md.Parser().Parse(reader)

	currentParent := root
	hasPreamble := false

	// Walk block-level children of the document node.
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		currentParent = processMarkdownBlock(child, source, root, currentParent, &hasPreamble)
	}

	// Assign requirement levels and sequential positions.
	globalPos := 0
	walkTree(root, func(n *StructuralNode) {
		if n.Text != "" {
			n.RequirementLevel = DetectRequirementLevel(n.Text)
		}
		n.Position = globalPos
		globalPos++
	})

	return root
}

// processMarkdownBlock handles a single block-level AST node, attaching results
// to the StructuralNode tree. Returns the new currentParent.
func processMarkdownBlock(node ast.Node, source []byte, root, currentParent *StructuralNode, hasPreamble *bool) *StructuralNode {
	switch n := node.(type) {
	case *ast.Heading:
		return processHeading(n, source, root, currentParent)

	case *ast.Paragraph:
		txt := extractInlineText(n, source)
		if txt == "" {
			return currentParent
		}
		if currentParent == root && len(root.Children) == 0 && !*hasPreamble {
			// Text before any heading → preamble node
			*hasPreamble = true
			preamble := &StructuralNode{
				UUID:     uuid.New().String(),
				NodeType: "clause",
				Title:    "Preamble",
				Text:     txt,
				Depth:    1,
				Parent:   root,
				Position: len(root.Children),
			}
			root.Children = append(root.Children, preamble)
			currentParent = preamble
		} else if m := subclauseInParagraphRe.FindStringSubmatch(txt); m != nil {
			// Numbered sub-clause without heading marker (e.g., "4.4.1 The organization shall...")
			numbering := m[1]
			rest := m[2]

			// Depth from dot count: "4.4.1" has 2 dots → depth 3
			depth := strings.Count(numbering, ".") + 1

			node := &StructuralNode{
				UUID:      uuid.New().String(),
				NodeType:  "subclause",
				Numbering: numbering,
				Depth:     depth,
				Text:      rest,
			}

			parent := findParent(root, currentParent, depth)
			node.Parent = parent
			node.Position = len(parent.Children)
			parent.Children = append(parent.Children, node)
			currentParent = node
		} else if m := noteInParagraphRe.FindStringSubmatch(txt); m != nil {
			// NOTE or EXAMPLE line → child node of current parent
			noteType := strings.ToLower(m[1]) // "note" or "example"
			noteNum := m[2]
			noteText := m[3]

			title := strings.ToUpper(noteType[:1]) + noteType[1:]
			if noteNum != "" {
				title += " " + noteNum
			}

			noteNode := &StructuralNode{
				UUID:     uuid.New().String(),
				NodeType: noteType,
				Title:    title,
				Text:     noteText,
				Depth:    currentParent.Depth + 1,
				Parent:   currentParent,
				Position: len(currentParent.Children),
			}
			currentParent.Children = append(currentParent.Children, noteNode)
		} else {
			appendText(currentParent, txt)
		}

	case *ast.FencedCodeBlock:
		lang := ""
		if n.Info != nil {
			lang = strings.TrimSpace(string(n.Info.Text(source)))
		}
		code := extractCodeBlockText(n, source)
		codeNode := &StructuralNode{
			UUID:     uuid.New().String(),
			NodeType: "code_block",
			Title:    lang,
			Text:     code,
			Depth:    currentParent.Depth + 1,
			Parent:   currentParent,
			Position: len(currentParent.Children),
		}
		currentParent.Children = append(currentParent.Children, codeNode)

	case *ast.CodeBlock: // indented code block
		code := extractCodeBlockText(n, source)
		codeNode := &StructuralNode{
			UUID:     uuid.New().String(),
			NodeType: "code_block",
			Text:     code,
			Depth:    currentParent.Depth + 1,
			Parent:   currentParent,
			Position: len(currentParent.Children),
		}
		currentParent.Children = append(currentParent.Children, codeNode)

	case *ast.List:
		processListNode(n, source, currentParent)

	case *ast.Blockquote:
		bq := processBlockquote(n, source, currentParent)
		currentParent.Children = append(currentParent.Children, bq)

	case *ast.ThematicBreak:
		// Skip horizontal rules

	case *ast.HTMLBlock:
		txt := extractRawText(n, source)
		if txt != "" {
			appendText(currentParent, txt)
		}

	default:
		// GFM table or other extension nodes
		if isTableNode(node) {
			txt := renderTableText(node, source)
			if txt != "" {
				appendText(currentParent, txt)
			}
		}
	}

	return currentParent
}

// processHeading creates a StructuralNode from a markdown heading, placing it
// at the correct depth in the tree.
func processHeading(heading *ast.Heading, source []byte, root, currentParent *StructuralNode) *StructuralNode {
	level := heading.Level
	title := extractInlineText(heading, source)

	nodeType := "clause"
	if level >= 2 {
		nodeType = "subclause"
	}

	numbering := ""
	// Extract leading numbering from heading text (e.g., "4.1.2 Understanding")
	if m := numberingInHeadingRe.FindStringSubmatch(title); m != nil {
		numbering = m[1]
		title = strings.TrimSpace(m[2])
	}

	node := &StructuralNode{
		UUID:      uuid.New().String(),
		NodeType:  nodeType,
		Numbering: numbering,
		Title:     title,
		Depth:     level,
	}

	// Navigate up the tree to find the correct parent
	parent := findParent(root, currentParent, level)
	node.Parent = parent
	node.Position = len(parent.Children)
	parent.Children = append(parent.Children, node)

	return node
}

// numberingInHeadingRe matches an optional leading dotted number like "4.1.2" or "4"
// followed by the rest of the heading title.
var numberingInHeadingRe = regexp.MustCompile(`^(\d+(?:\.\d+)*)\s+(.+)$`)

// subclauseInParagraphRe matches paragraphs starting with a multi-level numbered
// clause like "4.4.1 The organization shall..." (must have at least one dot).
var subclauseInParagraphRe = regexp.MustCompile(`^(\d+(?:\.\d+)+)\s+(.*)`)

// noteInParagraphRe matches NOTE/EXAMPLE lines at the start of a paragraph.
var noteInParagraphRe = regexp.MustCompile(`^(NOTE|EXAMPLE)\s*(\d*)\s+(.*)`)

// --- Boilerplate cleaning ---

// Patterns for lines to strip from MistralOCR (and similar) output.
var (
	copyrightLineRe  = regexp.MustCompile(`(?i)^©\s*ISO\s+\d{4}`)
	licensingLineRe  = regexp.MustCompile(`(?i)^Licensed to .+prohibited\.?$`)
	pageHeaderLineRe = regexp.MustCompile(`^ISO\s+\d+-?\d*:\d{4}\([A-Z]+\)\s*$`)
	imageRefLineRe   = regexp.MustCompile(`^!\[.*?\]\(.*?\)\s*$`)
)

// cleanMarkdownSource removes boilerplate lines (copyright, licensing, page
// headers, image refs) and the Table of Contents block from OCR-converted
// markdown before parsing.
func cleanMarkdownSource(source []byte) []byte {
	lines := strings.Split(string(source), "\n")
	var cleaned []string
	inTOC := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect and skip Table of Contents block
		if strings.HasPrefix(trimmed, "# Contents") || strings.HasPrefix(trimmed, "## Contents") {
			inTOC = true
			continue
		}
		if inTOC {
			// End TOC when we hit the next heading (that isn't part of TOC)
			if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") {
				inTOC = false
				// Fall through to process this heading normally
			} else {
				continue
			}
		}

		// Skip boilerplate lines
		if copyrightLineRe.MatchString(trimmed) {
			continue
		}
		if licensingLineRe.MatchString(trimmed) {
			continue
		}
		if pageHeaderLineRe.MatchString(trimmed) {
			continue
		}
		if imageRefLineRe.MatchString(trimmed) {
			continue
		}

		cleaned = append(cleaned, line)
	}

	return []byte(strings.Join(cleaned, "\n"))
}

// processListNode processes a goldmark List, creating list_item children on the parent.
func processListNode(list *ast.List, source []byte, parent *StructuralNode) {
	itemIdx := 0
	for child := list.FirstChild(); child != nil; child = child.NextSibling() {
		li, ok := child.(*ast.ListItem)
		if !ok {
			continue
		}

		var textParts []string
		var nestedLists []*ast.List

		// Iterate ListItem's children (paragraphs, nested lists, etc.)
		for inner := li.FirstChild(); inner != nil; inner = inner.NextSibling() {
			switch in := inner.(type) {
			case *ast.Paragraph:
				t := extractInlineText(in, source)
				if t != "" {
					textParts = append(textParts, t)
				}
			case *ast.List:
				nestedLists = append(nestedLists, in)
			default:
				t := extractBlockText(inner, source)
				if t != "" {
					textParts = append(textParts, t)
				}
			}
		}

		numbering := ""
		if list.IsOrdered() {
			numbering = fmt.Sprintf("%d)", list.Start+itemIdx)
		}

		node := &StructuralNode{
			UUID:      uuid.New().String(),
			NodeType:  "list_item",
			Numbering: numbering,
			Text:      strings.Join(textParts, "\n\n"),
			Depth:     parent.Depth + 1,
			Parent:    parent,
			Position:  len(parent.Children),
		}
		parent.Children = append(parent.Children, node)

		// Process nested lists as children of this list_item
		for _, nested := range nestedLists {
			processListNode(nested, source, node)
		}

		itemIdx++
	}
}

// processBlockquote creates a StructuralNode from a blockquote.
func processBlockquote(bq *ast.Blockquote, source []byte, parent *StructuralNode) *StructuralNode {
	var parts []string
	for child := bq.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *ast.Paragraph:
			t := extractInlineText(c, source)
			if t != "" {
				parts = append(parts, t)
			}
		default:
			t := extractBlockText(c, source)
			if t != "" {
				parts = append(parts, t)
			}
		}
	}

	return &StructuralNode{
		UUID:     uuid.New().String(),
		NodeType: "blockquote",
		Text:     strings.Join(parts, "\n\n"),
		Depth:    parent.Depth + 1,
		Parent:   parent,
		Position: len(parent.Children),
	}
}

// --- Text extraction helpers ---

// extractInlineText recursively extracts text from inline AST nodes.
func extractInlineText(node ast.Node, source []byte) string {
	var sb strings.Builder
	extractInlineTextRecursive(node, source, &sb)
	return strings.TrimSpace(sb.String())
}

func extractInlineTextRecursive(node ast.Node, source []byte, sb *strings.Builder) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *ast.Text:
			sb.Write(c.Text(source))
			if c.SoftLineBreak() || c.HardLineBreak() {
				sb.WriteByte('\n')
			}
		case *ast.String:
			sb.Write(c.Value)
		case *ast.CodeSpan:
			sb.WriteByte('`')
			extractInlineTextRecursive(c, source, sb)
			sb.WriteByte('`')
		case *ast.Link:
			sb.WriteByte('[')
			extractInlineTextRecursive(c, source, sb)
			sb.WriteString("](")
			sb.Write(c.Destination)
			sb.WriteByte(')')
		case *ast.Image:
			sb.WriteString("![")
			extractInlineTextRecursive(c, source, sb)
			sb.WriteString("](")
			sb.Write(c.Destination)
			sb.WriteByte(')')
		case *ast.AutoLink:
			sb.Write(c.URL(source))
		case *ast.Emphasis:
			// Preserve text content, skip formatting markers
			extractInlineTextRecursive(c, source, sb)
		case *ast.RawHTML:
			for i := 0; i < c.Segments.Len(); i++ {
				seg := c.Segments.At(i)
				sb.Write(seg.Value(source))
			}
		default:
			// Recurse for any other inline containers
			extractInlineTextRecursive(c, source, sb)
		}
	}
}

// extractCodeBlockText extracts the raw text content of a code block node.
func extractCodeBlockText(node ast.Node, source []byte) string {
	var sb strings.Builder
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		sb.Write(seg.Value(source))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// extractRawText extracts raw text from a block node using its Lines().
func extractRawText(node ast.Node, source []byte) string {
	var sb strings.Builder
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		sb.Write(seg.Value(source))
	}
	return strings.TrimSpace(sb.String())
}

// extractBlockText extracts text content from any block-level node.
func extractBlockText(node ast.Node, source []byte) string {
	switch n := node.(type) {
	case *ast.Paragraph:
		return extractInlineText(n, source)
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		return extractCodeBlockText(n, source)
	default:
		// For other block types, try raw lines first, then recurse into children
		if raw := extractRawText(node, source); raw != "" {
			return raw
		}
		var parts []string
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			if t := extractBlockText(child, source); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n\n")
	}
}

// --- Table rendering ---

// isTableNode checks if a node is a GFM table.
func isTableNode(node ast.Node) bool {
	_, ok := node.(*east.Table)
	return ok
}

// renderTableText renders a GFM table as pipe-delimited text.
func renderTableText(node ast.Node, source []byte) string {
	table, ok := node.(*east.Table)
	if !ok {
		return ""
	}

	var rows [][]string
	var alignments []east.Alignment

	for child := table.FirstChild(); child != nil; child = child.NextSibling() {
		switch row := child.(type) {
		case *east.TableHeader:
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				if tc, ok := cell.(*east.TableCell); ok {
					alignments = append(alignments, tc.Alignment)
				}
			}
			cells := extractTableRowCells(row, source)
			rows = append(rows, cells)
		case *east.TableRow:
			cells := extractTableRowCells(row, source)
			rows = append(rows, cells)
		}
	}

	if len(rows) == 0 {
		return ""
	}

	// Compute column widths
	colCount := 0
	for _, row := range rows {
		if len(row) > colCount {
			colCount = len(row)
		}
	}
	colWidths := make([]int, colCount)
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	var sb strings.Builder
	for rowIdx, row := range rows {
		sb.WriteString("| ")
		for i := 0; i < colCount; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			sb.WriteString(fmt.Sprintf("%-*s", colWidths[i], cell))
			sb.WriteString(" | ")
		}
		sb.WriteByte('\n')

		// Separator after header row
		if rowIdx == 0 {
			sb.WriteString("| ")
			for i := 0; i < colCount; i++ {
				sep := strings.Repeat("-", colWidths[i])
				if i < len(alignments) {
					switch alignments[i] {
					case east.AlignCenter:
						sep = ":" + strings.Repeat("-", colWidths[i]-2) + ":"
					case east.AlignRight:
						sep = strings.Repeat("-", colWidths[i]-1) + ":"
					case east.AlignLeft:
						sep = ":" + strings.Repeat("-", colWidths[i]-1)
					}
				}
				sb.WriteString(sep)
				sb.WriteString(" | ")
			}
			sb.WriteByte('\n')
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

// extractTableRowCells extracts cell text content from a table row node.
func extractTableRowCells(row ast.Node, source []byte) []string {
	var cells []string
	for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
		cells = append(cells, extractInlineText(cell, source))
	}
	return cells
}

// --- Utility ---

// appendText appends text to a node's Text field, separated by double newline.
func appendText(node *StructuralNode, text string) {
	if node.Text != "" {
		node.Text += "\n\n" + text
	} else {
		node.Text = text
	}
}

// MarkdownToStructuredChunks is a convenience function that parses markdown
// and produces chunks in one call. Used primarily for testing.
func MarkdownToStructuredChunks(source []byte, maxChunkSize, minChunkSize int) []StructuredChunk {
	root := ParseMarkdownStructure(source)
	return ChunkStructured(root, maxChunkSize, minChunkSize)
}
