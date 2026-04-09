package services

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// StructuralNode represents a hierarchical unit in a structured document
// (clause, article, section, annex, definition, etc.)
type StructuralNode struct {
	UUID             string
	NodeType         string // "document", "clause", "subclause", "article", "title", "chapter", "section", "paragraph", "annex", "note", "example", "terms_section", "definition", "list_item", "code_block", "blockquote"
	Numbering        string // "4.1.2", "Art. 12", "a)"
	Title            string // "Context of the organization"
	Depth            int    // 0=document root, 1=top-level section, 2=sub-section, ...
	Text             string // Full text content of this structural unit
	RequirementLevel string // "SHALL", "SHOULD", "MAY", ""
	Parent           *StructuralNode
	Children         []*StructuralNode
	Position         int // sequential position among siblings
}

// StructuredChunk is a chunk produced from the structural tree, carrying rich metadata.
type StructuredChunk struct {
	Text             string
	Position         int
	NodeType         string // inherited from the structural node
	Numbering        string
	FullPath         string // "Clause 4 > 4.1 Understanding > 4.1.2"
	RequirementLevel string
	Depth            int
	SectionUUID      string // UUID of the parent RagSection node
}

// CrossReference represents an intra-document cross-reference found in text.
type CrossReference struct {
	SourceText    string // the matched text, e.g. "see 4.1.3"
	TargetNumber  string // the resolved target numbering, e.g. "4.1.3"
	TargetType    string // "clause", "article", "annex", etc.
}

// headingRule maps a regex to a node type and how to extract numbering/title.
type headingRule struct {
	Pattern  *regexp.Regexp
	NodeType string
	// DepthFunc returns the structural depth for this heading.
	// If nil, depth is determined by the parser based on numbering.
	DepthFunc func(match []string) int
	// ExtractFunc extracts (numbering, title) from the match groups.
	ExtractFunc func(match []string) (string, string)
}

// --- Heading patterns (English + Italian) ---

var headingRules []headingRule

func init() {
	headingRules = []headingRule{
		// Italian legal: TITOLO I, TITOLO II, ...
		{
			Pattern:  regexp.MustCompile(`^(TITOLO\s+[IVXLCDM]+)\s*[—–-]?\s*(.*)$`),
			NodeType: "title",
			DepthFunc: func(_ []string) int { return 1 },
			ExtractFunc: func(m []string) (string, string) {
				return strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
			},
		},
		// Italian legal: CAPO I, CAPO II, ...
		{
			Pattern:  regexp.MustCompile(`^(CAPO\s+[IVXLCDM]+)\s*[—–-]?\s*(.*)$`),
			NodeType: "chapter",
			DepthFunc: func(_ []string) int { return 2 },
			ExtractFunc: func(m []string) (string, string) {
				return strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
			},
		},
		// Italian legal: SEZIONE I, SEZIONE II, ...
		{
			Pattern:  regexp.MustCompile(`^(SEZIONE\s+[IVXLCDM]+)\s*[—–-]?\s*(.*)$`),
			NodeType: "section",
			DepthFunc: func(_ []string) int { return 3 },
			ExtractFunc: func(m []string) (string, string) {
				return strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
			},
		},
		// Italian legal: Articolo 12, Art. 12
		{
			Pattern:  regexp.MustCompile(`^(?:Articolo|Art\.?)\s+(\d+)\s*[—–.-]?\s*(.*)$`),
			NodeType: "article",
			DepthFunc: func(_ []string) int { return 4 },
			ExtractFunc: func(m []string) (string, string) {
				return "Art. " + m[1], strings.TrimSpace(m[2])
			},
		},
		// English: Clause N, Section N, Article N
		{
			Pattern:  regexp.MustCompile(`^(Clause|Section|Article)\s+(\d+(?:\.\d+)*)\s*[—–.-]?\s*(.*)$`),
			NodeType: "clause",
			ExtractFunc: func(m []string) (string, string) {
				return m[2], strings.TrimSpace(m[3])
			},
		},
		// Annex A / ALLEGATO A
		{
			Pattern:  regexp.MustCompile(`^(?:Annex|ALLEGATO)\s+([A-Z])\s*[—–.-]?\s*(.*)$`),
			NodeType: "annex",
			DepthFunc: func(_ []string) int { return 1 },
			ExtractFunc: func(m []string) (string, string) {
				return "Annex " + m[1], strings.TrimSpace(m[2])
			},
		},
		// Numbered heading: 4.1.2.3 Title (most specific first: 4 dots, then 3, 2, 1)
		{
			Pattern:  regexp.MustCompile(`^(\d+\.\d+\.\d+\.\d+)\s+(.+)$`),
			NodeType: "subclause",
			ExtractFunc: func(m []string) (string, string) {
				return m[1], strings.TrimSpace(m[2])
			},
		},
		{
			Pattern:  regexp.MustCompile(`^(\d+\.\d+\.\d+)\s+(.+)$`),
			NodeType: "subclause",
			ExtractFunc: func(m []string) (string, string) {
				return m[1], strings.TrimSpace(m[2])
			},
		},
		{
			Pattern:  regexp.MustCompile(`^(\d+\.\d+)\s+(.+)$`),
			NodeType: "subclause",
			ExtractFunc: func(m []string) (string, string) {
				return m[1], strings.TrimSpace(m[2])
			},
		},
		// Top-level numbered clause: "4 Context of the organization"
		{
			Pattern:  regexp.MustCompile(`^(\d+)\s+([A-Z].+)$`),
			NodeType: "clause",
			DepthFunc: func(_ []string) int { return 1 },
			ExtractFunc: func(m []string) (string, string) {
				return m[1], strings.TrimSpace(m[2])
			},
		},
		// Well-known ISO section names
		{
			Pattern:  regexp.MustCompile(`^(Introduction|Scope|Normative references|Terms and definitions|Bibliography)$`),
			NodeType: "clause",
			DepthFunc: func(_ []string) int { return 1 },
			ExtractFunc: func(m []string) (string, string) {
				return "", m[1]
			},
		},
		// NOTE 1, NOTE, NOTA
		{
			Pattern:  regexp.MustCompile(`^(NOTE|NOTA)\s*(\d*)\s*[—–:-]?\s*(.*)$`),
			NodeType: "note",
			ExtractFunc: func(m []string) (string, string) {
				num := strings.TrimSpace(m[2])
				if num != "" {
					return "NOTE " + num, strings.TrimSpace(m[3])
				}
				return "NOTE", strings.TrimSpace(m[3])
			},
		},
		// EXAMPLE / ESEMPIO
		{
			Pattern:  regexp.MustCompile(`^(EXAMPLE|ESEMPIO)\s*(\d*)\s*[—–:-]?\s*(.*)$`),
			NodeType: "example",
			ExtractFunc: func(m []string) (string, string) {
				return m[1], strings.TrimSpace(m[3])
			},
		},
		// ALL CAPS heading (min 6 chars, not a note/example which are handled above)
		{
			Pattern:  regexp.MustCompile(`^([A-Z][A-Z\s]{5,})$`),
			NodeType: "clause",
			DepthFunc: func(_ []string) int { return 1 },
			ExtractFunc: func(m []string) (string, string) {
				return "", strings.TrimSpace(m[1])
			},
		},
	}
}

// numberingDepth returns the structural depth from a dotted numbering like "4.1.2".
func numberingDepth(numbering string) int {
	if numbering == "" {
		return 1
	}
	return strings.Count(numbering, ".") + 1
}

// ParseDocumentStructure parses extracted text into a hierarchical tree of StructuralNode.
func ParseDocumentStructure(text string) *StructuralNode {
	root := &StructuralNode{
		UUID:     uuid.New().String(),
		NodeType: "document",
		Title:    "Document",
		Depth:    0,
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return root
	}

	lines := strings.Split(text, "\n")

	// Process lines into paragraphs (groups of non-empty lines separated by blank lines)
	var paragraphs []string
	var currentPara strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if currentPara.Len() > 0 {
				paragraphs = append(paragraphs, currentPara.String())
				currentPara.Reset()
			}
			continue
		}
		if currentPara.Len() > 0 {
			currentPara.WriteString("\n")
		}
		currentPara.WriteString(trimmed)
	}
	if currentPara.Len() > 0 {
		paragraphs = append(paragraphs, currentPara.String())
	}

	// Build tree from paragraphs
	currentParent := root
	position := 0

	for _, para := range paragraphs {
		// Skip Markdown horizontal rules
		if strings.TrimSpace(para) == "---" || strings.TrimSpace(para) == "***" || strings.TrimSpace(para) == "___" {
			continue
		}

		firstLine := strings.SplitN(para, "\n", 2)[0]
		firstLine = strings.TrimSpace(firstLine)

		// Strip Markdown heading prefix (# ## ### etc.)
		firstLine = stripMarkdownHeading(firstLine)

		matched := false
		for _, rule := range headingRules {
			m := rule.Pattern.FindStringSubmatch(firstLine)
			if m == nil {
				continue
			}

			numbering, title := rule.ExtractFunc(m)
			depth := 0
			if rule.DepthFunc != nil {
				depth = rule.DepthFunc(m)
			} else {
				depth = numberingDepth(numbering)
			}

			node := &StructuralNode{
				UUID:     uuid.New().String(),
				NodeType: rule.NodeType,
				Numbering: numbering,
				Title:    title,
				Depth:    depth,
			}

			// Handle text body: if the paragraph has more content beyond the heading line
			parts := strings.SplitN(para, "\n", 2)
			if len(parts) > 1 {
				node.Text = strings.TrimSpace(stripMarkdownFromBody(parts[1]))
			}

			// Special handling for Terms and definitions
			if strings.Contains(strings.ToLower(title), "terms and definitions") ||
				strings.Contains(strings.ToLower(title), "definizioni") {
				node.NodeType = "terms_section"
			}

			// Navigate up the tree to find the correct parent
			// The parent should be the nearest ancestor with depth < this node's depth
			currentParent = findParent(root, currentParent, depth)

			node.Parent = currentParent
			node.Position = len(currentParent.Children)
			currentParent.Children = append(currentParent.Children, node)
			currentParent = node
			matched = true
			position++
			break
		}

		if !matched {
			// Not a heading — this is body text.
			// Check if it's a list item
			if isListItem(para) {
				node := &StructuralNode{
					UUID:     uuid.New().String(),
					NodeType: "list_item",
					Text:     para,
					Depth:    currentParent.Depth + 1,
					Parent:   currentParent,
					Position: len(currentParent.Children),
				}
				numbering := extractListNumbering(para)
				node.Numbering = numbering
				currentParent.Children = append(currentParent.Children, node)
			} else if currentParent == root && len(currentParent.Children) == 0 {
				// Text before any heading — attach directly to root
				node := &StructuralNode{
					UUID:     uuid.New().String(),
					NodeType: "clause",
					Title:    "Preamble",
					Text:     para,
					Depth:    1,
					Parent:   root,
					Position: len(root.Children),
				}
				root.Children = append(root.Children, node)
			} else {
				// Append text to current parent
				if currentParent.Text != "" {
					currentParent.Text += "\n\n" + para
				} else {
					currentParent.Text = para
				}
			}
		}
	}

	// Assign requirement levels and sequential positions
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

// findParent walks up from current to find the right parent for a node at the given depth.
func findParent(root, current *StructuralNode, depth int) *StructuralNode {
	p := current
	for p != root && p.Depth >= depth {
		p = p.Parent
	}
	return p
}

// walkTree traverses the tree depth-first, calling fn on each node.
func walkTree(node *StructuralNode, fn func(*StructuralNode)) {
	fn(node)
	for _, child := range node.Children {
		walkTree(child, fn)
	}
}

// CollectLeaves returns all leaf nodes (nodes with no children) in the tree.
func CollectLeaves(root *StructuralNode) []*StructuralNode {
	var leaves []*StructuralNode
	walkTree(root, func(n *StructuralNode) {
		if len(n.Children) == 0 && n != root {
			leaves = append(leaves, n)
		}
	})
	return leaves
}

// CollectSections returns all non-leaf, non-root nodes (sections that have children).
func CollectSections(root *StructuralNode) []*StructuralNode {
	var sections []*StructuralNode
	walkTree(root, func(n *StructuralNode) {
		if n != root && n.NodeType != "document" {
			sections = append(sections, n)
		}
	})
	return sections
}

// BuildFullPath builds the structural breadcrumb path for a node.
func BuildFullPath(node *StructuralNode) string {
	var parts []string
	n := node
	for n != nil && n.NodeType != "document" {
		label := n.Numbering
		if n.Title != "" {
			if label != "" {
				label += " " + n.Title
			} else {
				label = n.Title
			}
		}
		if label != "" {
			parts = append([]string{label}, parts...)
		}
		n = n.Parent
	}
	return strings.Join(parts, " > ")
}

// --- Requirement Level Detection ---

var requirementPatterns = []struct {
	Level   string
	Pattern *regexp.Regexp
}{
	// Check negated forms first
	// Note: \b doesn't work with Unicode chars (è, ò, etc.) in Go's regexp, so Italian
	// patterns use word-start/end matching without \b.
	{"SHALL_NOT", regexp.MustCompile(`(?i)(?:\bshall\s+not\b|non\s+deve|non\s+devono)`)},
	{"SHOULD_NOT", regexp.MustCompile(`(?i)(?:\bshould\s+not\b|non\s+dovrebbe|non\s+dovrebbero)`)},
	{"SHALL", regexp.MustCompile(`(?i)(?:\bshall\b|\bdeve\b|\bdevono\b|è\s+obbligatorio)`)},
	{"MUST", regexp.MustCompile(`(?i)(?:\bmust\b|è\s+necessario)`)},
	{"SHOULD", regexp.MustCompile(`(?i)(?:\bshould\b|\bdovrebbe\b|\bdovrebbero\b|è\s+opportuno)`)},
	{"MAY", regexp.MustCompile(`(?i)(?:\bmay\b|può|possono|è\s+consentito)`)},
}

// DetectRequirementLevel returns the highest-priority requirement level found in text.
func DetectRequirementLevel(text string) string {
	for _, rp := range requirementPatterns {
		if rp.Pattern.MatchString(text) {
			return rp.Level
		}
	}
	return ""
}

// --- Cross-Reference Extraction ---

var crossRefPatterns = []*regexp.Regexp{
	// English: "see 4.1.3", "refer to Clause 7", "in accordance with 4.2", "defined in 3.1", "specified in 4.1.3"
	regexp.MustCompile(`(?i)(?:see|refer\s+to|in\s+accordance\s+with|(?:as\s+)?(?:defined|described|specified)\s+in)\s+(?:Clause\s+)?(\d+(?:\.\d+)*)`),
	// English: "Article N", "Art. N"
	regexp.MustCompile(`(?i)(?:Article|Art\.?)\s+(\d+)`),
	// English: "Annex A"
	regexp.MustCompile(`(?i)(?:Annex|ALLEGATO)\s+([A-Z])`),
	// English: "Table N", "Figure N"
	regexp.MustCompile(`(?i)(?:Table|Tabella|Figure|Figura)\s+(\d+)`),
	// Italian: "comma N", "ai sensi del comma N"
	regexp.MustCompile(`(?i)(?:comma|paragrafo)\s+(\d+)`),
	// Italian: "articolo N del", "art. N"
	regexp.MustCompile(`(?i)(?:articolo|art\.?)\s+(\d+)`),
}

// ExtractCrossReferences finds all intra-document cross-references in the given text.
func ExtractCrossReferences(text string) []CrossReference {
	var refs []CrossReference
	seen := make(map[string]bool)

	for _, pattern := range crossRefPatterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, m := range matches {
			target := strings.TrimSpace(m[1])
			key := target
			if seen[key] {
				continue
			}
			seen[key] = true

			ref := CrossReference{
				SourceText:   m[0],
				TargetNumber: target,
			}

			// Determine target type
			lower := strings.ToLower(m[0])
			switch {
			case strings.Contains(lower, "annex") || strings.Contains(lower, "allegato"):
				ref.TargetType = "annex"
			case strings.Contains(lower, "article") || strings.Contains(lower, "art") || strings.Contains(lower, "articolo"):
				ref.TargetType = "article"
			case strings.Contains(lower, "table") || strings.Contains(lower, "tabella"):
				ref.TargetType = "table"
			case strings.Contains(lower, "figure") || strings.Contains(lower, "figura"):
				ref.TargetType = "figure"
			case strings.Contains(lower, "comma") || strings.Contains(lower, "paragrafo"):
				ref.TargetType = "paragraph"
			default:
				ref.TargetType = "clause"
			}

			refs = append(refs, ref)
		}
	}

	return refs
}

// --- Markdown handling ---

var markdownHeadingPattern = regexp.MustCompile(`^#{1,6}\s+`)

// stripMarkdownHeading removes Markdown heading prefix (# ## ### etc.) from a line.
func stripMarkdownHeading(line string) string {
	return strings.TrimSpace(markdownHeadingPattern.ReplaceAllString(line, ""))
}

// stripMarkdownFromBody strips Markdown formatting from body text:
// heading prefixes, bold/italic markers, but preserves content.
func stripMarkdownFromBody(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		// Strip heading prefixes from sub-lines
		lines[i] = markdownHeadingPattern.ReplaceAllString(line, "")
	}
	return strings.Join(lines, "\n")
}

// --- List item detection ---

var listItemPattern = regexp.MustCompile(`^(?:[a-z]\)|[0-9]+\)|—|–|-)\s+`)

func isListItem(text string) bool {
	return listItemPattern.MatchString(strings.TrimSpace(text))
}

func extractListNumbering(text string) string {
	text = strings.TrimSpace(text)
	m := regexp.MustCompile(`^([a-z]\)|[0-9]+\))`).FindString(text)
	return m
}
