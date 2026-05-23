package services

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// CodeFormatError signals a malformed CardType.CodeFormat template.
// The Position field carries the byte offset (0-indexed) of the
// offending character in the source template — surfaced via the
// admin UI so operators can spot the problem without scanning the
// whole string.
type CodeFormatError struct {
	Position int
	Message  string
}

func (e *CodeFormatError) Error() string {
	return fmt.Sprintf("card_code_format: %s at byte %d", e.Message, e.Position)
}

// ErrCodeFormatEmpty is returned by ParseCardCodeFormat when the
// caller passes the empty template. Wrapped by parse-site code in
// a CodeFormatError so the position is consistent.
var ErrCodeFormatEmpty = errors.New("card_code_format: empty template")

// CodeFormatLimit defines the upper bound on N for {seq:N} and
// {rand:N} placeholders. Beyond 12 characters the code crosses the
// natural readability boundary for operator-visible identifiers
// without adding entropy a human would actually verify, so the
// admin UI rejects anything wider at parse time.
const CodeFormatLimit = 12

// crockfordAlphabet is Crockford-Base32 — no I/L/O/U — so codes are
// safe to write down or read aloud without homograph confusion.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// CardCodeFormatAST is the parsed representation of a CodeFormat
// template, produced once by ParseCardCodeFormat and cached on the
// CardType for the lifetime of the process. RenderCardCode replays
// the AST with the supplied context to produce a concrete code.
//
// The AST is intentionally tiny — a flat slice of nodes evaluated in
// order — because the templates never need branching or loops.
type CardCodeFormatAST struct {
	nodes []codeNode
}

// HasSequence reports whether the template embeds a {seq:N}
// placeholder. CardService.Issue uses this to decide whether to
// reserve the next sequence value (and pay the findAndModify
// round-trip) at all — templates that consist purely of date parts
// and random digits do not need a counter.
func (a *CardCodeFormatAST) HasSequence() bool {
	if a == nil {
		return false
	}
	for _, n := range a.nodes {
		if n.kind == nodeSeq {
			return true
		}
	}
	return false
}

type codeNodeKind int

const (
	nodeLiteral codeNodeKind = iota
	nodeYYYY
	nodeYY
	nodeMM
	nodeDD
	nodeSeq
	nodeRand
)

type codeNode struct {
	kind codeNodeKind

	// literal carries the raw text for nodeLiteral nodes.
	literal string

	// width carries N for nodeSeq / nodeRand. Unused otherwise.
	width int
}

// ParseCardCodeFormat returns the AST of the supplied template or a
// CodeFormatError pointing at the offending byte. The function is
// pure — no time / rand reads — so it is safe to call from any code
// path. Call sites typically run this at CardType create / update;
// CardService caches the AST per type.
//
// Grammar (per IMPLEMENTATION_PLAN_PHASE_4.md §3.5):
//
//	template     = { literal | placeholder } ;
//	placeholder  = "{" ( datepart | seq | rand ) "}" ;
//	datepart     = "YYYY" | "YY" | "MM" | "DD" ;
//	seq          = "seq" ":" digit+ ;
//	rand         = "rand" ":" digit+ ;
//
// Validation rules:
//
//   - The template must not be empty.
//   - At most one {seq:N} placeholder is permitted (multiple
//     sequences make no operator sense).
//   - {seq:N} and {rand:N} require 1 ≤ N ≤ CodeFormatLimit.
//   - Unknown placeholder names ({foo}) fail parse.
//   - Unclosed placeholders ({YYYY...end-of-string) fail parse.
func ParseCardCodeFormat(tpl string) (*CardCodeFormatAST, error) {
	if tpl == "" {
		return nil, &CodeFormatError{Position: 0, Message: "empty template"}
	}

	ast := &CardCodeFormatAST{}
	seqCount := 0

	i := 0
	for i < len(tpl) {
		if tpl[i] != '{' {
			// Literal run — gather until the next '{' or end of string.
			j := i
			for j < len(tpl) && tpl[j] != '{' {
				j++
			}
			ast.nodes = append(ast.nodes, codeNode{kind: nodeLiteral, literal: tpl[i:j]})
			i = j
			continue
		}

		// Placeholder run — find the closing '}'.
		end := strings.IndexByte(tpl[i+1:], '}')
		if end == -1 {
			return nil, &CodeFormatError{Position: i, Message: "unterminated placeholder"}
		}
		body := tpl[i+1 : i+1+end]
		nextI := i + 1 + end + 1

		node, perr := parsePlaceholder(body, i)
		if perr != nil {
			return nil, perr
		}
		if node.kind == nodeSeq {
			seqCount++
			if seqCount > 1 {
				return nil, &CodeFormatError{
					Position: i,
					Message:  "more than one {seq:N} placeholder is not supported",
				}
			}
		}
		ast.nodes = append(ast.nodes, node)
		i = nextI
	}

	return ast, nil
}

func parsePlaceholder(body string, pos int) (codeNode, error) {
	if body == "" {
		return codeNode{}, &CodeFormatError{Position: pos, Message: "empty placeholder"}
	}
	switch body {
	case "YYYY":
		return codeNode{kind: nodeYYYY}, nil
	case "YY":
		return codeNode{kind: nodeYY}, nil
	case "MM":
		return codeNode{kind: nodeMM}, nil
	case "DD":
		return codeNode{kind: nodeDD}, nil
	}
	// seq:N or rand:N
	if idx := strings.IndexByte(body, ':'); idx > 0 {
		head := body[:idx]
		tail := body[idx+1:]
		width, werr := strconv.Atoi(tail)
		if werr != nil || width < 1 {
			return codeNode{}, &CodeFormatError{
				Position: pos,
				Message:  fmt.Sprintf("invalid width %q (expected integer ≥ 1)", tail),
			}
		}
		if width > CodeFormatLimit {
			return codeNode{}, &CodeFormatError{
				Position: pos,
				Message:  fmt.Sprintf("width %d exceeds CodeFormatLimit (%d)", width, CodeFormatLimit),
			}
		}
		switch head {
		case "seq":
			return codeNode{kind: nodeSeq, width: width}, nil
		case "rand":
			return codeNode{kind: nodeRand, width: width}, nil
		default:
			return codeNode{}, &CodeFormatError{
				Position: pos,
				Message:  fmt.Sprintf("unknown placeholder %q", head),
			}
		}
	}
	return codeNode{}, &CodeFormatError{
		Position: pos,
		Message:  fmt.Sprintf("unknown placeholder %q", body),
	}
}

// RenderCardCode evaluates a parsed template against the supplied
// (date, sequence) tuple and reads random bytes from `randSource`
// for any {rand:N} nodes. The function is deterministic when
// `randSource` is deterministic — used by tests to assert exact
// outputs.
//
// The caller supplies `seq` even if the template does not reference
// it (the value is then ignored); this keeps the signature simple
// and forces the caller to be explicit about whether a sequence
// reservation happened upstream.
func RenderCardCode(ast *CardCodeFormatAST, now time.Time, seq int64, randSource io.Reader) (string, error) {
	if ast == nil {
		return "", errors.New("card_code_format: nil AST")
	}
	if randSource == nil {
		randSource = rand.Reader
	}
	var b strings.Builder
	now = now.UTC()
	for _, n := range ast.nodes {
		switch n.kind {
		case nodeLiteral:
			b.WriteString(n.literal)
		case nodeYYYY:
			fmt.Fprintf(&b, "%04d", now.Year())
		case nodeYY:
			fmt.Fprintf(&b, "%02d", now.Year()%100)
		case nodeMM:
			fmt.Fprintf(&b, "%02d", int(now.Month()))
		case nodeDD:
			fmt.Fprintf(&b, "%02d", now.Day())
		case nodeSeq:
			// Zero-padded decimal. Width overflows are permitted — the
			// rendered code is wider than N characters, and the
			// (tenantId, code) unique index catches any collisions a
			// caller can produce by other means.
			fmt.Fprintf(&b, "%0*d", n.width, seq)
		case nodeRand:
			s, err := drawRandom(n.width, randSource)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
		default:
			return "", fmt.Errorf("card_code_format: unknown node kind %d", n.kind)
		}
	}
	return b.String(), nil
}

// drawRandom reads `width` Crockford-Base32 characters from
// `randSource`. The mapping uses rejection sampling so the
// distribution stays uniform across the 32-symbol alphabet (the
// alternative — `b & 0x1F` — biases the high two bits when the
// raw byte exceeds 32, an artifact small enough to matter for code
// uniqueness budgets).
func drawRandom(width int, randSource io.Reader) (string, error) {
	if width < 1 {
		return "", errors.New("card_code_format: rand width must be ≥ 1")
	}
	out := make([]byte, 0, width)
	buf := make([]byte, 1)
	for len(out) < width {
		if _, err := io.ReadFull(randSource, buf); err != nil {
			return "", err
		}
		// Top of the byte space (224..255) gets rejected so the
		// remaining 0..223 map uniformly onto 0..31 via & 0x1F.
		if buf[0] >= 224 {
			continue
		}
		out = append(out, crockfordAlphabet[buf[0]&0x1F])
	}
	return string(out), nil
}
