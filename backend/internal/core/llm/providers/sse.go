package providers

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// SSEEvent is one parsed server-sent event.
type SSEEvent struct {
	Event string
	Data  []byte
}

var ErrSSEEventTooLarge = errors.New("llm providers: SSE event exceeds the size limit")

// ReadSSE parses a text/event-stream, calling fn per event. Each event is
// capped at maxEventBytes (a single line, and the sum of its data lines) so a
// hostile upstream cannot grow memory unbounded; fn returning an error stops
// the read.
func ReadSSE(r io.Reader, maxEventBytes int, fn func(SSEEvent) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxEventBytes+1)
	var ev SSEEvent
	var data [][]byte
	size := 0
	flush := func() error {
		if len(data) == 0 && ev.Event == "" {
			return nil
		}
		ev.Data = bytes.Join(data, []byte("\n"))
		err := fn(ev)
		ev, data, size = SSEEvent{}, nil, 0
		return err
	}
	for sc.Scan() {
		line := sc.Bytes()
		switch {
		case len(line) == 0:
			if err := flush(); err != nil {
				return err
			}
		case bytes.HasPrefix(line, []byte(":")):
			// comment / keepalive
		case bytes.HasPrefix(line, []byte("event:")):
			ev.Event = string(bytes.TrimSpace(line[6:]))
		case bytes.HasPrefix(line, []byte("data:")):
			d := bytes.TrimSpace(line[5:])
			size += len(d) + 1
			if size > maxEventBytes {
				return ErrSSEEventTooLarge
			}
			data = append(data, bytes.Clone(d))
		}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return ErrSSEEventTooLarge
		}
		return err
	}
	return flush()
}
