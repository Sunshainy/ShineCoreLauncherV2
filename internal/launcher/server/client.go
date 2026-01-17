package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
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
	base := strings.TrimRight(c.BaseURL, "/")
	req, err := c.SignedRequest(ctx, http.MethodGet, base+"/manifest")
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("manifest request failed: " + resp.Status)
	}
	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
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
