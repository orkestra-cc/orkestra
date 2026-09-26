// Package gotenberg is the iface.PDFRenderer backed by a Gotenberg 8
// sidecar (Chromium HTML route). It never retries: the caller decides.
package gotenberg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	defaultTimeout     = 20 * time.Second
	defaultConcurrency = 4
	defaultMaxBytes    = 10 << 20
	mmPerInch          = 25.4
)

type Config struct {
	URL            string
	Username       string
	Password       string
	Timeout        time.Duration // per-call cap; the caller's deadline wins when sooner
	MaxConcurrency int
	MaxBytes       int64
	HTTPClient     *http.Client
}

type Client struct {
	cfg  Config
	http *http.Client
	sem  chan struct{}
}

var _ iface.PDFRenderer = (*Client)(nil)

func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = defaultConcurrency
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = defaultMaxBytes
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	return &Client{cfg: cfg, http: hc, sem: make(chan struct{}, cfg.MaxConcurrency)}
}

func inches(mm float64) string { return strconv.FormatFloat(mm/mmPerInch, 'f', 3, 64) }

func (c *Client) RenderHTML(ctx context.Context, doc iface.HTMLDocument) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: waiting for a slot: %w", iface.ErrPDFRendererUnavailable, ctx.Err())
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := writeFile(mw, "index.html", []byte(doc.HTML)); err != nil {
		return nil, fmt.Errorf("%w: %w", iface.ErrPDFRenderFailed, err)
	}
	for name, data := range doc.Assets {
		if err := writeFile(mw, name, data); err != nil {
			return nil, fmt.Errorf("%w: %w", iface.ErrPDFRenderFailed, err)
		}
	}
	p := doc.Paper.Normalized()
	w, h := p.DimensionsInches()
	fields := map[string]string{
		"paperWidth":        strconv.FormatFloat(w, 'f', 2, 64),
		"paperHeight":       strconv.FormatFloat(h, 'f', 2, 64),
		"marginTop":         inches(p.MarginTopMM),
		"marginRight":       inches(p.MarginRightMM),
		"marginBottom":      inches(p.MarginBottomMM),
		"marginLeft":        inches(p.MarginLeftMM),
		"printBackground":   "true",
		"preferCssPageSize": "false",
	}
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	_ = mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL+"/forms/chromium/convert/html", &body)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", iface.ErrPDFRenderFailed, err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.cfg.Username != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", iface.ErrPDFRendererUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: status %d", iface.ErrPDFRendererUnavailable, resp.StatusCode)
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("%w: status %d", iface.ErrPDFRenderFailed, resp.StatusCode)
	}
	out, err := io.ReadAll(io.LimitReader(resp.Body, c.cfg.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", iface.ErrPDFRendererUnavailable, err)
	}
	if int64(len(out)) > c.cfg.MaxBytes {
		return nil, iface.ErrPDFTooLarge
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		return nil, fmt.Errorf("%w: response is not a PDF", iface.ErrPDFRenderFailed)
	}
	return out, nil
}

// Health probes GET /health (used at boot and by the platform check).
func (c *Client) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.URL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", iface.ErrPDFRendererUnavailable, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: health status %d", iface.ErrPDFRendererUnavailable, resp.StatusCode)
	}
	return nil
}

func writeFile(mw *multipart.Writer, name string, data []byte) error {
	if strings.ContainsAny(name, "/\\") || name == "" {
		return errors.New("invalid asset name")
	}
	fw, err := mw.CreateFormFile("files", name)
	if err != nil {
		return err
	}
	_, err = fw.Write(data)
	return err
}
