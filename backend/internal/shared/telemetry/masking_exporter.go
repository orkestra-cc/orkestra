package telemetry

import (
	"context"
	"math"
	"reflect"
	"sync/atomic"

	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// spanKeyAliases maps OTel semantic-convention keys onto the masker's
// normalized key vocabulary (spec §2.5), so client.address is treated as an
// IP and user.id as a subject identifier.
var spanKeyAliases = map[string]string{
	"client.address":                      "ip",
	"net.peer.ip":                         "ip",
	"net.sock.peer.addr":                  "ip",
	"net.peer.addr":                       "ip",
	"network.peer.address":                "ip",
	"http.client_ip":                      "ip",
	"http.request.header.x-forwarded-for": "ip",
	"http.request.header.x-real-ip":       "ip",
	"user_agent.original":                 "useragent",
	"http.user_agent":                     "useragent",
	"user.id":                             "userid",
	"enduser.id":                          "userid",
}

// droppedSpanKeys carry the raw URL (path + query) and never leave the
// process; http.route identifies the endpoint instead.
var droppedSpanKeys = map[string]bool{"url.full": true, "http.url": true, "url.query": true}

// pathSpanKeys are replaced by http.route when the span has one and dropped
// when it has none (fail closed: a raw path carries identifiers and tokens).
var pathSpanKeys = map[string]bool{"url.path": true, "http.target": true}

type spanPolicyBox struct {
	p atomic.Pointer[utils.LogPolicyResolver]
}

var globalSpanBox atomic.Pointer[spanPolicyBox]

// SwapSpanPolicyResolver replaces the resolver of the exporter built by the
// most recent Init. main.go calls it once the compliance module is up.
func SwapSpanPolicyResolver(r utils.LogPolicyResolver) {
	if b := globalSpanBox.Load(); b != nil && r != nil {
		b.p.Store(&r)
	}
}

// fallbackSpanPolicy applies when no resolver answers: the default policy.
// (The resolver's own answer for a span without tenant.id is the strictest
// policy, spec §2.5.) Package-level so it is built once.
var fallbackSpanPolicy = iface.DefaultLogContentPolicy()

// MaskingExporter masks span, event and link attributes with the compliance
// policy of the span's tenant (attribute tenant.id; no tenant → whatever the
// resolver answers for "", the strictest policy) before handing them to the
// real exporter.
type MaskingExporter struct {
	next    sdktrace.SpanExporter
	box     *spanPolicyBox
	hashKey []byte
}

func NewMaskingExporter(next sdktrace.SpanExporter, r utils.LogPolicyResolver, hashKey []byte) *MaskingExporter {
	box := &spanPolicyBox{}
	if r != nil {
		box.p.Store(&r)
	}
	return &MaskingExporter{next: next, box: box, hashKey: hashKey}
}

func (e *MaskingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	out := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, s := range spans {
		out[i] = e.mask(s)
	}
	return e.next.ExportSpans(ctx, out)
}

func (e *MaskingExporter) Shutdown(ctx context.Context) error { return e.next.Shutdown(ctx) }

func (e *MaskingExporter) mask(s sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	stub := tracetest.SpanStubFromReadOnlySpan(s)
	p := e.policyFor(stub.Attributes)
	// The resolved route is read from the span's own attributes; events and
	// links reuse it for their (rare) path attributes.
	route := routeOf(stub.Attributes)
	stub.Attributes = e.maskAttributes(p, route, stub.Attributes)
	// Events and Links share their backing arrays with the original span
	// (other processors may still read it): copy before mutating.
	events := make([]sdktrace.Event, len(stub.Events))
	copy(events, stub.Events)
	for i := range events {
		events[i].Attributes = e.maskAttributes(p, route, events[i].Attributes)
	}
	stub.Events = events
	links := make([]sdktrace.Link, len(stub.Links))
	copy(links, stub.Links)
	for i := range links {
		links[i].Attributes = e.maskAttributes(p, route, links[i].Attributes)
	}
	stub.Links = links
	return stub.Snapshot()
}

func (e *MaskingExporter) policyFor(attrs []attribute.KeyValue) *iface.LogContentPolicy {
	tenant := ""
	for _, kv := range attrs {
		if kv.Key == "tenant.id" {
			tenant = kv.Value.AsString()
		}
	}
	if rp := e.box.p.Load(); rp != nil {
		if p := (*rp).LogContentFor(tenant); p != nil {
			return p
		}
	}
	return &fallbackSpanPolicy
}

func routeOf(attrs []attribute.KeyValue) string {
	route := ""
	for _, kv := range attrs {
		if kv.Key == "http.route" {
			route = kv.Value.AsString()
		}
	}
	return route
}

func (e *MaskingExporter) maskAttributes(p *iface.LogContentPolicy, route string, attrs []attribute.KeyValue) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		k := string(kv.Key)
		if droppedSpanKeys[k] {
			continue
		}
		if pathSpanKeys[k] {
			if route != "" {
				out = append(out, attribute.String(k, route))
			}
			continue
		}
		maskKey := k
		if alias, ok := spanKeyAliases[k]; ok {
			maskKey = alias
		}
		in := kv.Value.AsInterface()
		v, keep := utils.MaskKV(p, e.hashKey, maskKey, in)
		if !keep {
			continue
		}
		switch nv := v.(type) {
		case string:
			out = append(out, attribute.String(k, nv))
		case []string:
			out = append(out, attribute.StringSlice(k, nv))
		default:
			if unchanged(in, v) {
				out = append(out, kv)
			} else {
				// A result the exporter cannot represent faithfully: fail closed.
				out = append(out, attribute.String(k, "[REDACTED]"))
			}
		}
	}
	return out
}

// unchanged reports whether the masker handed back the input as it was
// (NaN floats compare unequal to themselves, hence the special case).
func unchanged(in, out any) bool {
	if a, ok := in.(float64); ok {
		if b, ok := out.(float64); ok && math.IsNaN(a) && math.IsNaN(b) {
			return true
		}
	}
	return reflect.DeepEqual(in, out)
}

// newGlobalMaskingExporter wraps exp with the boot defaults and registers
// its resolver box for SwapSpanPolicyResolver.
func newGlobalMaskingExporter(exp sdktrace.SpanExporter) *MaskingExporter {
	m := NewMaskingExporter(exp, utils.NewStaticLogPolicyResolver(iface.DefaultLogContentPolicy()), utils.LogHashKeyFromEnv())
	globalSpanBox.Store(m.box)
	return m
}
