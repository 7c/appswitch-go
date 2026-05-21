package appswitch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

type statsEntry struct {
	Path     string `json:"path"`
	Host     string `json:"host"`
	Version  string `json:"version"`
	Count    int    `json:"count"`
	LastSeen string `json:"lastSeen"`
}

type snapshotResult struct {
	notModified bool
	response    *keysResponse
	etag        string
}

type transport struct {
	cfg    resolvedConfig
	client *http.Client
}

func newTransport(cfg resolvedConfig) *transport {
	return &transport{cfg: cfg, client: &http.Client{Timeout: cfg.requestTimeout}}
}

func (t *transport) do(
	ctx context.Context,
	method, path string,
	body io.Reader,
	headers map[string]string,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, t.cfg.endpoint+path, body)
	if err != nil {
		return nil, wrapError(CodeNetwork, "build request", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.cfg.apiKey)
	req.Header.Set("User-Agent", t.cfg.userAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	res, err := t.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, wrapError(CodeTimeout, "request to "+path+" timed out", err)
		}
		return nil, wrapError(CodeNetwork, "request to "+path+" failed", err)
	}
	return res, nil
}

func (t *transport) errorFrom(res *http.Response, path string) *Error {
	code := CodeNetwork
	message := path + " returned " + res.Status
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if data, err := io.ReadAll(res.Body); err == nil {
		if json.Unmarshal(data, &body) == nil {
			if body.Code != "" {
				code = Code(body.Code)
			}
			if body.Message != "" {
				message = body.Message
			}
		}
	}
	return newError(code, message)
}

// fetchSnapshot does GET /v1/keys with a conditional If-None-Match.
func (t *transport) fetchSnapshot(ctx context.Context, etag string) (snapshotResult, error) {
	headers := map[string]string{}
	if etag != "" {
		headers["If-None-Match"] = etag
	}
	res, err := t.do(ctx, http.MethodGet, "/v1/keys", nil, headers)
	if err != nil {
		return snapshotResult{}, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotModified {
		return snapshotResult{notModified: true, etag: etag}, nil
	}
	if res.StatusCode != http.StatusOK {
		return snapshotResult{}, t.errorFrom(res, "/v1/keys")
	}

	var resp keysResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return snapshotResult{}, wrapError(CodeNetwork, "decode /v1/keys", err)
	}
	return snapshotResult{response: &resp, etag: res.Header.Get("ETag")}, nil
}

// fetchKey does GET /v1/keys/:path (single key, rare/lazy path).
func (t *transport) fetchKey(ctx context.Context, keyPath string) (*ResolvedKey, error) {
	res, err := t.do(ctx, http.MethodGet, "/v1/keys/"+keyPath, nil, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, t.errorFrom(res, "/v1/keys/"+keyPath)
	}
	var key ResolvedKey
	if err := json.NewDecoder(res.Body).Decode(&key); err != nil {
		return nil, wrapError(CodeNetwork, "decode key", err)
	}
	return &key, nil
}

// postStats flushes telemetry. Best-effort: errors are returned but the caller
// typically ignores them (CLIENT.md §4).
func (t *transport) postStats(ctx context.Context, entries []statsEntry) error {
	if len(entries) == 0 {
		return nil
	}
	payload, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		return err
	}
	res, err := t.do(ctx, http.MethodPost, "/v1/_stats", bytes.NewReader(payload),
		map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)
	return nil
}
