package telemetry

import (
	"context"
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

// pathSpanKeys are replaced by http.route when the span has one.
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

// MaskingExporter masks span and event attributes with the compliance
// policy of the span's tenant (attribute tenant.id; none → strictest)
// before handing them to the real exporter.
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
	stub.Attributes = e.maskAttributes(p, stub.Attributes)
	for i := range stub.Events {
		stub.Events[i].Attributes = e.maskAttributes(p, stub.Events[i].Attributes)
	}
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
	fallback := iface.DefaultLogContentPolicy()
	return &fallback
}

func (e *MaskingExporter) maskAttributes(p *iface.LogContentPolicy, attrs []attribute.KeyValue) []attribute.KeyValue {
	route := ""
	for _, kv := range attrs {
		if kv.Key == "http.route" {
			route = kv.Value.AsString()
		}
	}
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		k := string(kv.Key)
		if droppedSpanKeys[k] {
			continue
		}
		if pathSpanKeys[k] && route != "" {
			out = append(out, attribute.String(k, route))
			continue
		}
		maskKey := k
		if alias, ok := spanKeyAliases[k]; ok {
			maskKey = alias
		}
		v, keep := utils.MaskKV(p, e.hashKey, maskKey, kv.Value.AsInterface())
		if !keep {
			continue
		}
		if s, ok := v.(string); ok {
			out = append(out, attribute.String(k, s))
		} else {
			out = append(out, kv) // non-string values are never rewritten
		}
	}
	return out
}

// newGlobalMaskingExporter wraps exp with the boot defaults and registers
// its resolver box for SwapSpanPolicyResolver.
func newGlobalMaskingExporter(exp sdktrace.SpanExporter) *MaskingExporter {
	m := NewMaskingExporter(exp, utils.NewStaticLogPolicyResolver(iface.DefaultLogContentPolicy()), utils.LogHashKeyFromEnv())
	globalSpanBox.Store(m.box)
	return m
}
