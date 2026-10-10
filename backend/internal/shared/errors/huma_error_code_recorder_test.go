package errors

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// The code of an errcode.Error written by a handler must land in the
// request's annotations holder, and only an error that opted in through
// errcode.FeatureNotConfigured may mark the response as an expected
// unavailability — a "*_not_configured" code alone must not.
func TestRecordErrorCode_StoresCodeAndOptInInAnnotations(t *testing.T) {
	cfg := huma.DefaultConfig("test", "1.0")
	cfg.Transformers = append(cfg.Transformers, RecordErrorCode())
	_, api := humatest.New(t, cfg)

	routes := map[string]error{
		"/optin": errcode.FeatureNotConfigured("x.y_not_configured", "not configured"),
		"/coded": errcode.ServiceUnavailable(errcode.AuthJWTNotConfigured, "signing keys missing"),
		"/plain": huma.Error503ServiceUnavailable("down"),
	}
	for path, err := range routes {
		huma.Register(api, huma.Operation{OperationID: path[1:], Method: http.MethodGet, Path: path},
			func(context.Context, *struct{}) (*struct{}, error) { return nil, err })
	}

	for path, want := range map[string]ctxauth.AnnotationSnapshot{
		"/optin": {ErrorCode: "x.y_not_configured", ExpectedUnavailable: true},
		"/coded": {ErrorCode: errcode.AuthJWTNotConfigured},
		"/plain": {},
	} {
		ctx, ann := ctxauth.WithRequestAnnotations(context.Background())
		resp := api.GetCtx(ctx, path)
		if resp.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, want 503", path, resp.Code)
		}
		if got := ann.Snapshot(); got != want {
			t.Errorf("%s: recorded %+v, want %+v", path, got, want)
		}
	}
}
