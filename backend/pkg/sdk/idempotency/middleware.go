package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

const (
	HeaderKey      = "Idempotency-Key"
	HeaderReplayed = "Idempotent-Replayed"

	defaultMaxBody   = 1 << 20
	maxStoredBody    = 256 << 10
	defaultLockTTL   = 2 * time.Minute
	defaultRetention = 24 * time.Hour
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

// Options configures Middleware.
type Options struct {
	Scope     string // e.g. "operator" / "client"; prefixed to the stored scope
	Required  bool   // refuse mutating requests without the header
	MaxBody   int64  // default 1 MiB
	LockTTL   time.Duration
	Retention time.Duration
	Now       func() time.Time
	Logger    *slog.Logger
}

// Middleware applies Idempotency-Key semantics to mutating requests.
func Middleware(store Store, opt Options) func(http.Handler) http.Handler {
	if opt.MaxBody <= 0 {
		opt.MaxBody = defaultMaxBody
	}
	if opt.LockTTL <= 0 {
		opt.LockTTL = defaultLockTTL
	}
	if opt.Retention <= 0 {
		opt.Retention = defaultRetention
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Logger == nil {
		opt.Logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			key := r.Header.Get(HeaderKey)
			if key == "" {
				if opt.Required {
					writeProblem(w, http.StatusBadRequest, CodeKeyRequired, "the Idempotency-Key header is required")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if !keyPattern.MatchString(key) {
				writeProblem(w, http.StatusBadRequest, CodeInvalidKey, "Idempotency-Key must be 8-128 characters of [A-Za-z0-9._:-]")
				return
			}
			user, ok := ctxauth.GetUserUUID(r.Context())
			if !ok || user == "" {
				writeProblem(w, http.StatusBadRequest, CodeUnauthenticated, "Idempotency-Key requires an authenticated caller")
				return
			}
			tenant, _ := ctxauth.GetTenantID(r.Context())
			scope := opt.Scope + "|" + tenant + "|" + user

			body, err := io.ReadAll(io.LimitReader(r.Body, opt.MaxBody+1))
			if err != nil {
				writeProblem(w, http.StatusBadRequest, CodeInvalidKey, "cannot read request body")
				return
			}
			if int64(len(body)) > opt.MaxBody {
				writeProblem(w, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, "request body too large for an idempotent request")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			sum := sha256.New()
			sum.Write([]byte(r.Method + "\n" + r.URL.RequestURI() + "\n"))
			sum.Write(body)
			fp := hex.EncodeToString(sum.Sum(nil))

			now := opt.Now()
			owner, existing, err := store.Begin(r.Context(), scope, key, fp, now, now.Add(opt.LockTTL), now.Add(opt.Retention))
			if err != nil {
				opt.Logger.Error("idempotency store begin failed", "error", err)
				writeProblem(w, http.StatusServiceUnavailable, CodeUnavailable, "idempotency store unavailable")
				return
			}
			if existing != nil {
				replay(w, existing, fp)
				return
			}

			// Release/finish outlive a client disconnect.
			sctx := context.WithoutCancel(r.Context())
			abandon := func() {
				if ok, aerr := store.Abandon(sctx, scope, key, owner); aerr != nil {
					opt.Logger.Error("idempotency abandon failed", "error", aerr)
				} else if !ok {
					opt.Logger.Warn("idempotency abandon skipped: lock was taken over", "scope", scope)
				}
			}
			// Keep the lock alive while the handler runs.
			stopRenew := make(chan struct{})
			renewDone := make(chan struct{})
			go func() {
				defer close(renewDone)
				t := time.NewTicker(opt.LockTTL / 3)
				defer t.Stop()
				for {
					select {
					case <-stopRenew:
						return
					case <-t.C:
						ok, rerr := store.Renew(sctx, scope, key, owner, opt.Now().Add(opt.LockTTL))
						if rerr != nil {
							opt.Logger.Error("idempotency renew failed", "error", rerr)
						} else if !ok {
							opt.Logger.Warn("idempotency renew: lock was taken over", "scope", scope)
							return
						}
					}
				}
			}()
			stop := func() {
				close(stopRenew)
				<-renewDone
			}
			cw := &captureWriter{ResponseWriter: w}
			defer func() {
				if rec := recover(); rec != nil {
					stop()
					abandon()
					panic(rec)
				}
			}()
			next.ServeHTTP(cw, r)
			stop()
			if !cw.wrote || cw.status >= 500 {
				abandon()
				return
			}
			h := cw.Header()
			var stored []byte
			if !cw.overflow {
				stored = cw.buf.Bytes()
			}
			ok, ferr := store.Finish(sctx, scope, key, owner, cw.status, h.Get("Content-Type"), h.Get("Location"), stored, !cw.overflow)
			if ferr != nil {
				opt.Logger.Error("idempotency finish failed", "error", ferr)
			} else if !ok {
				opt.Logger.Warn("idempotency finish skipped: lock was taken over", "scope", scope)
			}
		})
	}
}

func replay(w http.ResponseWriter, rec *Record, fp string) {
	if rec.Fingerprint != fp {
		writeProblem(w, http.StatusUnprocessableEntity, CodeKeyReused, "Idempotency-Key was already used with a different request")
		return
	}
	if rec.State != StateDone {
		w.Header().Set("Retry-After", "1")
		writeProblem(w, http.StatusConflict, CodeInProgress, "a request with this Idempotency-Key is still in progress")
		return
	}
	if !rec.BodyStored {
		writeProblem(w, http.StatusConflict, CodeReplayUnavailable, "the original response is too large to replay")
		return
	}
	h := w.Header()
	if rec.ContentType != "" {
		h.Set("Content-Type", rec.ContentType)
	}
	if rec.Location != "" {
		h.Set("Location", rec.Location)
	}
	h.Set(HeaderReplayed, "true")
	w.WriteHeader(rec.Status)
	_, _ = w.Write(rec.Body)
}

// captureWriter records the status and (bounded) body while passing through.
type captureWriter struct {
	http.ResponseWriter
	status   int
	wrote    bool
	buf      bytes.Buffer
	overflow bool
}

func (c *captureWriter) WriteHeader(code int) {
	if !c.wrote {
		c.wrote = true
		c.status = code
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *captureWriter) Write(p []byte) (int, error) {
	if !c.wrote {
		c.wrote = true
		c.status = http.StatusOK
	}
	if !c.overflow {
		if c.buf.Len()+len(p) > maxStoredBody {
			c.overflow = true
			c.buf.Reset()
		} else {
			c.buf.Write(p)
		}
	}
	return c.ResponseWriter.Write(p)
}

func (c *captureWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *captureWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }
