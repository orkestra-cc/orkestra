package providers

import (
	"errors"
	"strings"
	"testing"
)

func TestReadSSE_ParsesEventsCommentsAndMultilineData(t *testing.T) {
	in := ": keepalive\nevent: message_start\ndata: {\"a\":1}\n\ndata: line1\ndata: line2\n\ndata: [DONE]\n\n"
	var got []SSEEvent
	err := ReadSSE(strings.NewReader(in), 1024, func(e SSEEvent) error { got = append(got, e); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Event != "message_start" || string(got[0].Data) != `{"a":1}` ||
		string(got[1].Data) != "line1\nline2" || string(got[2].Data) != "[DONE]" {
		t.Fatalf("got %+v", got)
	}
}

func TestReadSSE_FlushesTrailingEventWithoutBlankLine(t *testing.T) {
	var got []SSEEvent
	if err := ReadSSE(strings.NewReader("data: tail"), 1024, func(e SSEEvent) error { got = append(got, e); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got[0].Data) != "tail" {
		t.Fatalf("got %+v", got)
	}
}

func TestReadSSE_EventTooLarge(t *testing.T) {
	err := ReadSSE(strings.NewReader("data: "+strings.Repeat("x", 4096)+"\n\n"), 1024, func(SSEEvent) error { return nil })
	if !errors.Is(err, ErrSSEEventTooLarge) {
		t.Fatalf("err = %v", err)
	}
	// many small lines accumulating into one event are capped too
	big := strings.Repeat("data: "+strings.Repeat("x", 100)+"\n", 40) + "\n"
	err = ReadSSE(strings.NewReader(big), 1024, func(SSEEvent) error { return nil })
	if !errors.Is(err, ErrSSEEventTooLarge) {
		t.Fatalf("accumulated err = %v", err)
	}
}

func TestReadSSE_CallbackErrorStops(t *testing.T) {
	stop := errors.New("stop")
	n := 0
	err := ReadSSE(strings.NewReader("data: a\n\ndata: b\n\n"), 1024, func(SSEEvent) error { n++; return stop })
	if !errors.Is(err, stop) || n != 1 {
		t.Fatalf("err=%v n=%d", err, n)
	}
}
