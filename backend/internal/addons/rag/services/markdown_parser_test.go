package services

import (
	"strings"
	"testing"
)

func TestParseMarkdownStructure_Headings(t *testing.T) {
	md := []byte(`# Introduction

Some intro text.

## Getting Started

Getting started guide.

### Prerequisites

You need Go 1.21+.

### Installation

Run go install.

## Configuration

Config details here.
`)

	root := ParseMarkdownStructure(md)

	if root.NodeType != "document" {
		t.Fatalf("expected root 'document', got %q", root.NodeType)
	}

	// Should have 1 top-level heading: "Introduction"
	if len(root.Children) != 1 {
		t.Fatalf("expected 1 top-level child, got %d", len(root.Children))
	}

	intro := root.Children[0]
	if intro.Title != "Introduction" {
		t.Errorf("expected title 'Introduction', got %q", intro.Title)
	}
	if intro.NodeType != "clause" {
		t.Errorf("expected h1 to be 'clause', got %q", intro.NodeType)
	}
	if intro.Depth != 1 {
		t.Errorf("expected depth 1, got %d", intro.Depth)
	}
	if !strings.Contains(intro.Text, "Some intro text") {
		t.Errorf("expected intro body text, got %q", intro.Text)
	}

	// "Introduction" should have 2 h2 children: "Getting Started", "Configuration"
	if len(intro.Children) != 2 {
		t.Fatalf("expected 2 h2 children under Introduction, got %d", len(intro.Children))
	}

	gs := intro.Children[0]
	if gs.Title != "Getting Started" || gs.NodeType != "subclause" || gs.Depth != 2 {
		t.Errorf("unexpected h2: title=%q type=%q depth=%d", gs.Title, gs.NodeType, gs.Depth)
	}

	// "Getting Started" should have 2 h3 children
	if len(gs.Children) != 2 {
		t.Fatalf("expected 2 h3 children under Getting Started, got %d", len(gs.Children))
	}

	prereqs := gs.Children[0]
	if prereqs.Title != "Prerequisites" || prereqs.Depth != 3 {
		t.Errorf("unexpected h3: title=%q depth=%d", prereqs.Title, prereqs.Depth)
	}

	config := intro.Children[1]
	if config.Title != "Configuration" || config.Depth != 2 {
		t.Errorf("unexpected h2: title=%q depth=%d", config.Title, config.Depth)
	}
}

func TestParseMarkdownStructure_ParagraphAccumulation(t *testing.T) {
	md := []byte(`# Section

First paragraph of the section.

Second paragraph continues here.

Third paragraph with more details.
`)

	root := ParseMarkdownStructure(md)

	section := root.Children[0]
	paragraphs := strings.Split(section.Text, "\n\n")
	if len(paragraphs) != 3 {
		t.Errorf("expected 3 paragraphs, got %d: %q", len(paragraphs), section.Text)
	}
}

func TestParseMarkdownStructure_CodeBlocks(t *testing.T) {
	md := []byte("# Setup\n\nSome text.\n\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\nMore text after code.\n")

	root := ParseMarkdownStructure(md)

	setup := root.Children[0]

	// Should have a code_block child
	var codeBlock *StructuralNode
	for _, child := range setup.Children {
		if child.NodeType == "code_block" {
			codeBlock = child
			break
		}
	}

	if codeBlock == nil {
		t.Fatal("expected a code_block child node")
	}
	if codeBlock.Title != "go" {
		t.Errorf("expected language 'go', got %q", codeBlock.Title)
	}
	if !strings.Contains(codeBlock.Text, "fmt.Println") {
		t.Errorf("expected code content, got %q", codeBlock.Text)
	}

	// Paragraphs before and after code should be in setup.Text
	if !strings.Contains(setup.Text, "Some text") {
		t.Errorf("expected paragraph before code in parent text, got %q", setup.Text)
	}
	if !strings.Contains(setup.Text, "More text after code") {
		t.Errorf("expected paragraph after code in parent text, got %q", setup.Text)
	}
}

func TestParseMarkdownStructure_UnorderedList(t *testing.T) {
	md := []byte(`# Features

- Fast performance
- Easy to use
- Extensible
`)

	root := ParseMarkdownStructure(md)
	features := root.Children[0]

	listItems := 0
	for _, child := range features.Children {
		if child.NodeType == "list_item" {
			listItems++
			if child.Numbering != "" {
				t.Errorf("unordered list item should have no numbering, got %q", child.Numbering)
			}
		}
	}
	if listItems != 3 {
		t.Errorf("expected 3 list items, got %d", listItems)
	}
}

func TestParseMarkdownStructure_OrderedList(t *testing.T) {
	md := []byte(`# Steps

1. Clone the repo
2. Run install
3. Start the server
`)

	root := ParseMarkdownStructure(md)
	steps := root.Children[0]

	var items []*StructuralNode
	for _, child := range steps.Children {
		if child.NodeType == "list_item" {
			items = append(items, child)
		}
	}

	if len(items) != 3 {
		t.Fatalf("expected 3 ordered list items, got %d", len(items))
	}
	if items[0].Numbering != "1)" {
		t.Errorf("expected numbering '1)', got %q", items[0].Numbering)
	}
	if items[2].Numbering != "3)" {
		t.Errorf("expected numbering '3)', got %q", items[2].Numbering)
	}
	if !strings.Contains(items[0].Text, "Clone the repo") {
		t.Errorf("expected list item text, got %q", items[0].Text)
	}
}

func TestParseMarkdownStructure_NestedList(t *testing.T) {
	md := []byte(`# Items

- Parent item
  - Nested child 1
  - Nested child 2
- Another parent
`)

	root := ParseMarkdownStructure(md)
	items := root.Children[0]

	// Should have 2 top-level list items
	topLevel := 0
	for _, child := range items.Children {
		if child.NodeType == "list_item" {
			topLevel++
			// First item should have nested children
			if strings.Contains(child.Text, "Parent item") && len(child.Children) != 2 {
				t.Errorf("expected 2 nested children under 'Parent item', got %d", len(child.Children))
			}
		}
	}
	if topLevel != 2 {
		t.Errorf("expected 2 top-level list items, got %d", topLevel)
	}
}

func TestParseMarkdownStructure_Blockquote(t *testing.T) {
	md := []byte(`# Notes

> This is a blockquote.
> It spans multiple lines.

Regular text after.
`)

	root := ParseMarkdownStructure(md)
	notes := root.Children[0]

	var bq *StructuralNode
	for _, child := range notes.Children {
		if child.NodeType == "blockquote" {
			bq = child
			break
		}
	}

	if bq == nil {
		t.Fatal("expected a blockquote child node")
	}
	if !strings.Contains(bq.Text, "blockquote") {
		t.Errorf("expected blockquote text, got %q", bq.Text)
	}
}

func TestParseMarkdownStructure_Table(t *testing.T) {
	md := []byte(`# Data

| Name  | Value |
|-------|-------|
| alpha | 1     |
| beta  | 2     |
`)

	root := ParseMarkdownStructure(md)
	data := root.Children[0]

	if !strings.Contains(data.Text, "alpha") || !strings.Contains(data.Text, "beta") {
		t.Errorf("expected table content in text, got %q", data.Text)
	}
	if !strings.Contains(data.Text, "|") {
		t.Errorf("expected pipe-delimited table, got %q", data.Text)
	}
}

func TestParseMarkdownStructure_ThematicBreak(t *testing.T) {
	md := []byte(`# First

Content.

---

# Second

More content.
`)

	root := ParseMarkdownStructure(md)

	if len(root.Children) != 2 {
		t.Fatalf("expected 2 top-level sections, got %d", len(root.Children))
	}
	if root.Children[0].Title != "First" {
		t.Errorf("expected 'First', got %q", root.Children[0].Title)
	}
	if root.Children[1].Title != "Second" {
		t.Errorf("expected 'Second', got %q", root.Children[1].Title)
	}
}

func TestParseMarkdownStructure_InlineFormatting(t *testing.T) {
	md := []byte(`# Section

This has **bold** and *italic* and ` + "`code`" + ` inline.
`)

	root := ParseMarkdownStructure(md)
	section := root.Children[0]

	if !strings.Contains(section.Text, "bold") {
		t.Errorf("expected 'bold' in text, got %q", section.Text)
	}
	if !strings.Contains(section.Text, "italic") {
		t.Errorf("expected 'italic' in text, got %q", section.Text)
	}
	if !strings.Contains(section.Text, "`code`") {
		t.Errorf("expected '`code`' in text, got %q", section.Text)
	}
}

func TestParseMarkdownStructure_Links(t *testing.T) {
	md := []byte(`# Resources

Check [the docs](https://example.com) for details.
`)

	root := ParseMarkdownStructure(md)
	section := root.Children[0]

	if !strings.Contains(section.Text, "[the docs](https://example.com)") {
		t.Errorf("expected link preserved in text, got %q", section.Text)
	}
}

func TestParseMarkdownStructure_Preamble(t *testing.T) {
	md := []byte(`This text comes before any heading.

# First Section

Section content.
`)

	root := ParseMarkdownStructure(md)

	if len(root.Children) < 2 {
		t.Fatalf("expected at least 2 children (preamble + section), got %d", len(root.Children))
	}

	preamble := root.Children[0]
	if preamble.Title != "Preamble" {
		t.Errorf("expected 'Preamble' title, got %q", preamble.Title)
	}
	if !strings.Contains(preamble.Text, "before any heading") {
		t.Errorf("expected preamble text, got %q", preamble.Text)
	}
}

func TestParseMarkdownStructure_RequirementLevels(t *testing.T) {
	md := []byte(`# Requirements

## Mandatory

The system shall validate all inputs.

## Optional

The system may provide additional logging.
`)

	root := ParseMarkdownStructure(md)

	mandatory := root.Children[0].Children[0]
	if mandatory.RequirementLevel != "SHALL" {
		t.Errorf("expected SHALL, got %q", mandatory.RequirementLevel)
	}

	optional := root.Children[0].Children[1]
	if optional.RequirementLevel != "MAY" {
		t.Errorf("expected MAY, got %q", optional.RequirementLevel)
	}
}

func TestParseMarkdownStructure_EmptyDocument(t *testing.T) {
	for _, input := range [][]byte{nil, {}, []byte(""), []byte("   \n\n  ")} {
		root := ParseMarkdownStructure(input)
		if root.NodeType != "document" {
			t.Errorf("expected root 'document', got %q", root.NodeType)
		}
		if len(root.Children) != 0 {
			t.Errorf("expected no children for empty input, got %d", len(root.Children))
		}
	}
}

func TestParseMarkdownStructure_NumberedHeadings(t *testing.T) {
	md := []byte(`# 4 Context

## 4.1 Understanding

### 4.1.2 Requirements

The organization shall comply.
`)

	root := ParseMarkdownStructure(md)

	clause := root.Children[0]
	if clause.Numbering != "4" || clause.Title != "Context" {
		t.Errorf("expected '4' 'Context', got %q %q", clause.Numbering, clause.Title)
	}

	sub := clause.Children[0]
	if sub.Numbering != "4.1" || sub.Title != "Understanding" {
		t.Errorf("expected '4.1' 'Understanding', got %q %q", sub.Numbering, sub.Title)
	}

	subsub := sub.Children[0]
	if subsub.Numbering != "4.1.2" || subsub.Title != "Requirements" {
		t.Errorf("expected '4.1.2' 'Requirements', got %q %q", subsub.Numbering, subsub.Title)
	}
}

func TestParseMarkdownStructure_EndToEnd_WithChunker(t *testing.T) {
	md := []byte(`# Project Overview

This project implements a RAG pipeline for ISO compliance.

## Architecture

The system uses a modular monolith design with strict module boundaries.

### Backend

Go 1.21+ with Huma v2 framework. The organization shall maintain module isolation.

### Frontend

React 19 with TypeScript and Vite.

## Configuration

### Environment Variables

Set the following variables:

- ` + "`DATABASE_URL`" + `: MongoDB connection string
- ` + "`REDIS_URL`" + `: Redis connection string
- ` + "`PORT`" + `: Server port (default 3000)

### Docker Setup

` + "```yaml\nservices:\n  backend:\n    build: ./backend\n    ports:\n      - \"3000:3000\"\n```" + `

Run with docker compose up.
`)

	chunks := MarkdownToStructuredChunks(md, 1024, 128)

	if len(chunks) == 0 {
		t.Fatal("expected chunks, got none")
	}

	// All chunks should have FullPath set
	for i, chunk := range chunks {
		if chunk.FullPath == "" {
			t.Errorf("chunk %d has empty FullPath", i)
		}
	}

	// Should detect SHALL requirement
	foundShall := false
	for _, chunk := range chunks {
		if chunk.RequirementLevel == "SHALL" {
			foundShall = true
			break
		}
	}
	if !foundShall {
		t.Error("expected to find a chunk with SHALL requirement level")
	}

	// Should have code_block chunk
	foundCode := false
	for _, chunk := range chunks {
		if chunk.NodeType == "code_block" {
			foundCode = true
			break
		}
	}
	if !foundCode {
		t.Error("expected to find a code_block chunk")
	}

	// Verify structural paths contain expected hierarchy
	foundArchPath := false
	for _, chunk := range chunks {
		if strings.Contains(chunk.FullPath, "Architecture") && strings.Contains(chunk.FullPath, "Backend") {
			foundArchPath = true
			break
		}
	}
	if !foundArchPath {
		t.Error("expected to find a chunk with path containing 'Architecture > Backend'")
	}
}

func TestParseMarkdownStructure_DepthSkip(t *testing.T) {
	// Test heading levels that skip (e.g., h1 -> h3)
	md := []byte(`# Top

### Deep without h2

Content here.
`)

	root := ParseMarkdownStructure(md)

	top := root.Children[0]
	if len(top.Children) != 1 {
		t.Fatalf("expected 1 child under h1, got %d", len(top.Children))
	}

	deep := top.Children[0]
	if deep.Title != "Deep without h2" {
		t.Errorf("expected 'Deep without h2', got %q", deep.Title)
	}
	// h3 should still be a child of h1 since there's no h2
	if deep.Depth != 3 {
		t.Errorf("expected depth 3, got %d", deep.Depth)
	}
}
