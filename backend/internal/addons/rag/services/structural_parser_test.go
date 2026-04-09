package services

import (
	"strings"
	"testing"
)

func TestParseDocumentStructure_ISODocument(t *testing.T) {
	text := `1 Scope

This International Standard specifies requirements for a quality management system.

2 Normative references

The following documents are referred to in the text.

3 Terms and definitions

For the purposes of this document, the following terms and definitions apply.

4 Context of the organization

4.1 Understanding the organization and its context

The organization shall determine external and internal issues.

4.1.1 General

The organization shall monitor and review information about these issues.

4.1.2 Requirements

The organization should consider risk-based thinking.

5 Leadership

5.1 Leadership and commitment

Top management shall demonstrate leadership and commitment.
`

	root := ParseDocumentStructure(text)

	if root.NodeType != "document" {
		t.Fatalf("expected root node type 'document', got %q", root.NodeType)
	}

	// Should have top-level clauses: 1, 2, 3, 4, 5
	if len(root.Children) != 5 {
		t.Fatalf("expected 5 top-level children, got %d", len(root.Children))
	}

	// Clause 1: Scope
	scope := root.Children[0]
	if scope.Numbering != "1" || scope.Title != "Scope" {
		t.Errorf("expected clause '1 Scope', got %q %q", scope.Numbering, scope.Title)
	}

	// Clause 3: Terms and definitions -> should be terms_section
	terms := root.Children[2]
	if terms.NodeType != "terms_section" {
		t.Errorf("expected 'terms_section' node type for clause 3, got %q", terms.NodeType)
	}

	// Clause 4: should have children 4.1
	clause4 := root.Children[3]
	if clause4.Numbering != "4" {
		t.Errorf("expected numbering '4', got %q", clause4.Numbering)
	}
	if len(clause4.Children) == 0 {
		t.Fatal("clause 4 should have children")
	}

	// 4.1 should have children 4.1.1 and 4.1.2
	sub41 := clause4.Children[0]
	if sub41.Numbering != "4.1" {
		t.Errorf("expected numbering '4.1', got %q", sub41.Numbering)
	}
	if len(sub41.Children) < 2 {
		t.Fatalf("4.1 should have at least 2 children, got %d", len(sub41.Children))
	}
}

func TestParseDocumentStructure_ItalianLaw(t *testing.T) {
	text := `TITOLO I — Disposizioni generali

CAPO I — Ambito di applicazione

Articolo 1 — Oggetto e finalità

La presente legge disciplina il trattamento dei dati personali.

Articolo 2 — Definizioni

Ai fini della presente legge si intende per dato personale qualsiasi informazione.

CAPO II — Principi

Articolo 3 — Principi generali

Il trattamento dei dati personali deve essere effettuato nel rispetto della dignità umana.

TITOLO II — Diritti dell'interessato

SEZIONE I — Diritto di accesso

Articolo 4 — Diritto di informazione

L'interessato ha il diritto di ottenere informazioni sul trattamento.
`

	root := ParseDocumentStructure(text)

	// Should have 2 top-level TITOLO nodes
	if len(root.Children) != 2 {
		t.Fatalf("expected 2 top-level children (TITOLO), got %d", len(root.Children))
	}

	titolo1 := root.Children[0]
	if titolo1.NodeType != "title" {
		t.Errorf("expected 'title' node type, got %q", titolo1.NodeType)
	}
	if !strings.Contains(titolo1.Numbering, "TITOLO I") {
		t.Errorf("expected numbering containing 'TITOLO I', got %q", titolo1.Numbering)
	}

	// TITOLO I should have 2 CAPO children
	if len(titolo1.Children) != 2 {
		t.Fatalf("TITOLO I should have 2 children (CAPO), got %d", len(titolo1.Children))
	}

	capo1 := titolo1.Children[0]
	if capo1.NodeType != "chapter" {
		t.Errorf("expected 'chapter' node type, got %q", capo1.NodeType)
	}

	// CAPO I should have 2 Articolo children
	if len(capo1.Children) != 2 {
		t.Fatalf("CAPO I should have 2 Articolo children, got %d", len(capo1.Children))
	}

	art1 := capo1.Children[0]
	if art1.NodeType != "article" || art1.Numbering != "Art. 1" {
		t.Errorf("expected article 'Art. 1', got type=%q numbering=%q", art1.NodeType, art1.Numbering)
	}
}

func TestDetectRequirementLevel(t *testing.T) {
	tests := []struct {
		text     string
		expected string
	}{
		{"The organization shall determine external issues.", "SHALL"},
		{"The organization shall not disclose information.", "SHALL_NOT"},
		{"Top management should review the system.", "SHOULD"},
		{"The organization may use alternative methods.", "MAY"},
		{"L'organizzazione deve determinare le questioni.", "SHALL"},
		{"Non deve essere divulgata.", "SHALL_NOT"},
		{"L'organizzazione dovrebbe considerare.", "SHOULD"},
		{"L'organizzazione può utilizzare metodi alternativi.", "MAY"},
		{"This is a general description with no requirements.", ""},
		{"The organization must comply with regulations.", "MUST"},
	}

	for _, tt := range tests {
		result := DetectRequirementLevel(tt.text)
		if result != tt.expected {
			t.Errorf("DetectRequirementLevel(%q) = %q, want %q", tt.text, result, tt.expected)
		}
	}
}

func TestExtractCrossReferences(t *testing.T) {
	text := `The organization shall comply with the requirements specified in 4.1.3.
See Clause 7 for further details.
As defined in Annex A, the following applies.
Refer to Article 12 for the definitions.
In accordance with 5.2.1, the organization shall ensure compliance.`

	refs := ExtractCrossReferences(text)

	if len(refs) < 4 {
		t.Fatalf("expected at least 4 cross-references, got %d", len(refs))
	}

	// Check that we found the key references
	targets := make(map[string]bool)
	for _, ref := range refs {
		targets[ref.TargetNumber] = true
	}

	expected := []string{"4.1.3", "7", "A", "12", "5.2.1"}
	for _, e := range expected {
		if !targets[e] {
			t.Errorf("expected cross-reference to %q but not found", e)
		}
	}
}

func TestBuildFullPath(t *testing.T) {
	root := &StructuralNode{NodeType: "document"}
	clause := &StructuralNode{
		NodeType:  "clause",
		Numbering: "4",
		Title:     "Context",
		Parent:    root,
	}
	sub := &StructuralNode{
		NodeType:  "subclause",
		Numbering: "4.1",
		Title:     "Understanding",
		Parent:    clause,
	}
	subsub := &StructuralNode{
		NodeType:  "subclause",
		Numbering: "4.1.2",
		Title:     "Requirements",
		Parent:    sub,
	}

	path := BuildFullPath(subsub)
	expected := "4 Context > 4.1 Understanding > 4.1.2 Requirements"
	if path != expected {
		t.Errorf("BuildFullPath = %q, want %q", path, expected)
	}
}

func TestChunkStructured_BasicISO(t *testing.T) {
	text := `1 Scope

This International Standard specifies requirements for a quality management system.

2 Normative references

The following referenced documents are indispensable.

3 Terms and definitions

For the purposes of this document, the terms apply.

4 Context of the organization

4.1 Understanding the organization and its context

The organization shall determine external and internal issues that are relevant to its purpose.

4.2 Understanding the needs and expectations of interested parties

The organization should determine the interested parties that are relevant.
`

	root := ParseDocumentStructure(text)
	chunks := ChunkStructured(root, 1024, 128)

	if len(chunks) == 0 {
		t.Fatal("expected chunks, got none")
	}

	// Verify all chunks have full paths
	for i, chunk := range chunks {
		if chunk.FullPath == "" {
			t.Errorf("chunk %d has empty FullPath", i)
		}
	}

	// Check that requirement levels are detected
	foundShall := false
	foundShould := false
	for _, chunk := range chunks {
		if chunk.RequirementLevel == "SHALL" {
			foundShall = true
		}
		if chunk.RequirementLevel == "SHOULD" {
			foundShould = true
		}
	}
	if !foundShall {
		t.Error("expected to find a chunk with SHALL requirement level")
	}
	if !foundShould {
		t.Error("expected to find a chunk with SHOULD requirement level")
	}
}

func TestChunkStructured_LargeTextSplitsAtSentences(t *testing.T) {
	// Create a large block of text
	sentences := make([]string, 50)
	for i := range sentences {
		sentences[i] = "This is a sentence that contributes to the overall text content of this section."
	}
	largeText := strings.Join(sentences, " ")

	root := &StructuralNode{
		UUID:     "root",
		NodeType: "document",
		Children: []*StructuralNode{
			{
				UUID:     "clause1",
				NodeType: "clause",
				Numbering: "1",
				Title:    "Test Clause",
				Text:     largeText,
				Depth:    1,
			},
		},
	}
	root.Children[0].Parent = root

	chunks := ChunkStructured(root, 512, 128)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks from large text, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		if len(chunk.Text) > 600 { // allow some margin for sentence boundaries
			t.Errorf("chunk %d exceeds expected size: %d chars", i, len(chunk.Text))
		}
	}
}
