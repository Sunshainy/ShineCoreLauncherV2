package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Secret  string
	Client  *http.Client
}

func (c *Client) FetchManifest(ctx context.Context) (*Manifest, error) {
	manifest, raw, err := c.fetchManifestOnline(ctx)
	if err == nil && manifest != nil {
		_ = c.saveCachedManifest(raw)
		return manifest, nil
	}
	if cached, cacheErr := c.loadCachedManifest(); cacheErr == nil {
		return cached, nil
	}
	return nil, err
}

func (c *Client) manifestCachePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(configDir, "shinecore")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "manifest_cache.json"), nil
}

func (c *Client) saveCachedManifest(data []byte) error {
	manifestPath, err := c.manifestCachePath()
	if err != nil {
		return err
	}
	metaPath, err := c.manifestCacheMetaPath()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(manifestPath, data, 0o644); err != nil {
		return err
	}
	meta := cacheMeta{FetchedAtUnix: time.Now().Unix()}
	payload, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return writeFileAtomic(metaPath, payload, 0o644)
}

func (c *Client) loadCachedManifest() (*Manifest, error) {
	path, err := c.manifestCachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (c *Client) fetchManifestOnline(ctx context.Context) (*Manifest, []byte, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := c.httpClient()
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if reqCtx.Err() != nil {
			return nil, nil, reqCtx.Err()
		}
		req, err := c.SignedRequest(reqCtx, http.MethodGet, base+"/manifest")
		if err != nil {
			return nil, nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			sleepWithContext(reqCtx, time.Duration(attempt)*300*time.Millisecond)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				return nil, nil, errors.New("manifest unavailable: server error " + resp.Status)
			}
			lastErr = errors.New("manifest unavailable: server error " + resp.Status)
			sleepWithContext(reqCtx, time.Duration(attempt)*300*time.Millisecond)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			sleepWithContext(reqCtx, time.Duration(attempt)*300*time.Millisecond)
			continue
		}
		var manifest Manifest
		if err := json.Unmarshal(body, &manifest); err != nil {
			return nil, nil, err
		}
		return &manifest, body, nil
	}
	return nil, nil, lastErr
}

type cacheMeta struct {
	FetchedAtUnix int64 `json:"fetched_at_unix"`
}

func (c *Client) manifestCacheMetaPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(configDir, "shinecore")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "manifest_cache.meta.json"), nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}

func sleepWithContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (c *Client) SignedRequest(ctx context.Context, method, urlStr string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, urlStr, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(c.Secret) == "" {
		return req, nil
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	ts := time.Now().Unix()
	sign := signRequest(c.Secret, method, u.Path, ts)
	req.Header.Set("X-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Signature", sign)
	return req, nil
}

func (c *Client) IsLocalDownload(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	if u.IsAbs() {
		return false
	}
	return strings.HasPrefix(path.Clean(u.Path), "/download/")
}

func (c *Client) ResolveURL(raw string) string {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	base := strings.TrimRight(c.BaseURL, "/")
	return base + "/" + strings.TrimLeft(raw, "/")
}

func (c *Client) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return http.DefaultClient
}

func signRequest(secret, method, reqPath string, timestamp int64) string {
	message := method + "\n" + reqPath + "\n" + strconv.FormatInt(timestamp, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
