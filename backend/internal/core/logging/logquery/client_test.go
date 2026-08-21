package logquery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/logging/models"
)

var fixedNow = time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)

func TestClient_QueryBuildsOnlyConstrainedLogQL(t *testing.T) {
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	t.Cleanup(server.Close)

	client := newClient(server.URL, []string{"auth"}, 3*time.Second, func() time.Time { return fixedNow })
	_, err := client.Query(context.Background(), Query{
		Module:        "auth",
		WindowMinutes: 15,
		Level:         "error",
		Text:          "quote\" line\nnext",
		Limit:         250,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	req := <-requests
	if req.URL.Path != "/loki/api/v1/query_range" {
		t.Fatalf("path = %q, want fixed Loki query-range path", req.URL.Path)
	}
	if req.Method != http.MethodGet {
		t.Errorf("method = %s, want GET", req.Method)
	}
	wantQuery := `{service="orkestra-backend", module="auth"} | json | level="ERROR" |= "quote\" line\nnext"`
	if got := req.URL.Query().Get("query"); got != wantQuery {
		t.Errorf("LogQL = %q, want %q", got, wantQuery)
	}
	if got := req.URL.Query().Get("start"); got != "1787226300000000000" {
		t.Errorf("start = %q, want fixed 15-minute boundary", got)
	}
	if got := req.URL.Query().Get("end"); got != "1787227200000000000" {
		t.Errorf("end = %q, want fixed current time", got)
	}
	if got := req.URL.Query().Get("limit"); got != "100" {
		t.Errorf("limit = %q, want clamped 100", got)
	}
	if got := req.URL.Query().Get("direction"); got != "backward" {
		t.Errorf("direction = %q, want backward", got)
	}
	if strings.Contains(req.URL.RawQuery, "quote\"") || strings.Contains(req.URL.RawQuery, "\nnext") {
		t.Errorf("raw query contains unencoded search syntax: %s", req.URL.RawQuery)
	}
}

func TestClient_QueryAcceptsOnlyClosedWindows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	t.Cleanup(server.Close)
	client := newClient(server.URL, []string{"auth"}, 3*time.Second, func() time.Time { return fixedNow })

	for _, minutes := range []int{5, 15, 60} {
		t.Run(fmt.Sprintf("%d minutes", minutes), func(t *testing.T) {
			_, err := client.Query(context.Background(), Query{Module: "auth", WindowMinutes: minutes, Limit: 20})
			if err != nil {
				t.Fatalf("allowed window %d rejected: %v", minutes, err)
			}
		})
	}

	for _, minutes := range []int{0, 1, 10, 61} {
		t.Run(fmt.Sprintf("reject %d", minutes), func(t *testing.T) {
			_, err := client.Query(context.Background(), Query{Module: "auth", WindowMinutes: minutes, Limit: 20})
			if !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf("window %d error = %v, want ErrInvalidQuery", minutes, err)
			}
		})
	}
}

func TestClient_QueryRejectsInvalidInputBeforeUpstreamIO(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	t.Cleanup(server.Close)
	client := newClient(server.URL, []string{"auth"}, 3*time.Second, time.Now)

	tests := []struct {
		name  string
		query Query
	}{
		{name: "unknown module", query: Query{Module: `auth"} |= "pwned`, WindowMinutes: 15, Limit: 20}},
		{name: "invalid level", query: Query{Module: "auth", WindowMinutes: 15, Level: `error" or "debug`, Limit: 20}},
		{name: "search over 200 characters", query: Query{Module: "auth", WindowMinutes: 15, Text: strings.Repeat("é", 201), Limit: 20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Query(context.Background(), tt.query)
			if !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf("error = %v, want ErrInvalidQuery", err)
			}
		})
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("upstream calls = %d, want zero for rejected input", got)
	}
}

func TestClient_QueryNormalizesAndMinimizesEventsChronologically(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"status":"success",
			"data":{"resultType":"streams","result":[{
				"stream":{"module":"auth"},
				"values":[
					["1787227199000000000", "{\"level\":\"ERROR\",\"msg\":\"second user@example.com\",\"module\":\"spoofed\",\"trace_id\":\"trace-2\",\"span_id\":\"span-2\",\"request_id\":\"req-2\",\"route\":\"/v1/users\",\"duration_ms\":12.5,\"password\":\"hunter2\",\"arbitrary\":\"drop me\"}"],
					["1787227198000000000", "{\"level\":\"INFO\",\"msg\":\"first\",\"trace_id\":\"trace-1\",\"duration_ms\":3}"]
				]
			}]}
		}`))
	}))
	t.Cleanup(server.Close)
	client := newClient(server.URL, []string{"auth"}, 3*time.Second, func() time.Time { return fixedNow })

	events, err := client.Query(context.Background(), Query{Module: "auth", WindowMinutes: 5, Limit: 20})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if !events[0].Timestamp.Before(events[1].Timestamp) || events[0].Message != "first" {
		t.Errorf("events not normalized chronologically: %+v", events)
	}
	second := events[1]
	if second.Level != models.LogLevelError || second.Module != "auth" {
		t.Errorf("normalized level/module = %q/%q, want error/auth", second.Level, second.Module)
	}
	if second.Message != "second user@example.com" {
		t.Errorf("message = %q, want preserved free text (not claimed PII-free)", second.Message)
	}
	wantAttributes := map[string]any{
		"trace_id":    "trace-2",
		"span_id":     "span-2",
		"request_id":  "req-2",
		"route":       "/v1/users",
		"duration_ms": 12.5,
	}
	if !reflect.DeepEqual(second.Attributes, wantAttributes) {
		t.Errorf("attributes = %#v, want allowlisted %#v", second.Attributes, wantAttributes)
	}
	if _, ok := second.Attributes["password"]; ok {
		t.Error("credential field survived the allowlisted projection")
	}
	if _, ok := second.Attributes["arbitrary"]; ok {
		t.Error("non-allowlisted field survived the projection")
	}
}

func TestClient_QueryAcceptsLokiV3StructuredMetadataTuple(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"status":"success",
			"data":{"resultType":"streams","result":[{
				"stream":{"module":"auth"},
				"values":[
					["1787227199000000000", "{\"level\":\"INFO\",\"msg\":\"metadata-bearing event\",\"trace_id\":\"line-trace\"}", {"trace_id":"metadata-trace","password":"must-not-surface"}]
				]
			}]}
		}`))
	}))
	t.Cleanup(server.Close)
	client := newClient(server.URL, []string{"auth"}, 3*time.Second, func() time.Time { return fixedNow })

	events, err := client.Query(context.Background(), Query{Module: "auth", WindowMinutes: 5, Limit: 20})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Message != "metadata-bearing event" {
		t.Errorf("message = %q, want metadata-bearing event", events[0].Message)
	}
	if got := events[0].Attributes["trace_id"]; got != "line-trace" {
		t.Errorf("trace_id = %#v, want allowlisted line value; structured metadata must be ignored", got)
	}
	if _, ok := events[0].Attributes["password"]; ok {
		t.Error("structured metadata credential field survived projection")
	}
}

func TestClient_QueryBoundsReturnedEventsEvenWhenUpstreamDoesNot(t *testing.T) {
	values := make([]string, 0, 101)
	for i := 0; i < 101; i++ {
		values = append(values, fmt.Sprintf(`["%d","{\"level\":\"INFO\",\"msg\":\"event\"}"]`, 1787227000000000000+int64(i)))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"streams","result":[{"stream":{},"values":[%s]}]}}`, strings.Join(values, ","))
	}))
	t.Cleanup(server.Close)
	client := newClient(server.URL, []string{"auth"}, 3*time.Second, func() time.Time { return fixedNow })

	events, err := client.Query(context.Background(), Query{Module: "auth", WindowMinutes: 5, Limit: 500})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 100 {
		t.Errorf("events = %d, want hard cap 100", len(events))
	}
}

func TestClient_QueryMapsUpstreamFailuresWithoutBodyDisclosure(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr error
	}{
		{
			name: "non-2xx",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("upstream-secret-body"))
			},
			wantErr: ErrUpstream,
		},
		{
			name: "invalid JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"status":"success","data":`))
			},
			wantErr: ErrUpstream,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			t.Cleanup(server.Close)
			client := newClient(server.URL, []string{"auth"}, 3*time.Second, time.Now)

			_, err := client.Query(context.Background(), Query{Module: "auth", WindowMinutes: 15, Limit: 20})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if strings.Contains(fmt.Sprint(err), "upstream-secret-body") {
				t.Errorf("error disclosed upstream response body: %v", err)
			}
		})
	}
}

func TestClient_QueryResponseSizeBoundary(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		wantErr error
	}{
		{name: "accepts exactly one MiB", size: maxResponseBytes},
		{name: "rejects one MiB plus one byte", size: maxResponseBytes + 1, wantErr: ErrResponseTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := lokiResponseWithSize(t, tt.size)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(server.Close)
			client := newClient(server.URL, []string{"auth"}, 3*time.Second, time.Now)

			_, err := client.Query(context.Background(), Query{Module: "auth", WindowMinutes: 15, Limit: 20})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_QueryTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	t.Cleanup(server.Close)
	client := newClient(server.URL, []string{"auth"}, 20*time.Millisecond, time.Now)

	_, err := client.Query(context.Background(), Query{Module: "auth", WindowMinutes: 15, Limit: 20})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("error = %v, want ErrTimeout", err)
	}
}

func TestClient_QueryMapsTimeoutWhileReadingBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("response writer does not support flushing")
			return
		}
		flusher.Flush()
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	t.Cleanup(server.Close)
	client := newClient(server.URL, []string{"auth"}, 20*time.Millisecond, time.Now)

	_, err := client.Query(context.Background(), Query{Module: "auth", WindowMinutes: 15, Limit: 20})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("error = %v, want ErrTimeout", err)
	}
}

func TestProviderAbsenceDegradesWithoutNetworkAccess(t *testing.T) {
	provider := New("", []string{"auth"})
	if status := provider.Status(context.Background()); status.Available {
		t.Errorf("blank provider status = %+v, want unavailable", status)
	}
	_, err := provider.Query(context.Background(), Query{Module: "auth", WindowMinutes: 15, Limit: 20})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestClient_DoesNotFollowUpstreamRedirects(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(true)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	t.Cleanup(target.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	client := newClient(server.URL, []string{"auth"}, 3*time.Second, time.Now)

	_, err := client.Query(context.Background(), Query{Module: "auth", WindowMinutes: 15, Limit: 20})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
	if redirected.Load() {
		t.Error("dedicated client followed an upstream redirect")
	}
}

func lokiResponseWithSize(t *testing.T, size int) string {
	t.Helper()
	base := `{"status":"success","data":{"resultType":"streams","result":[]}}`
	if len(base) > size {
		t.Fatalf("base response length = %d, exceeds requested size %d", len(base), size)
	}
	return base + strings.Repeat(" ", size-len(base))
}
