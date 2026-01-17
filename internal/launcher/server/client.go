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
	base := strings.TrimRight(c.BaseURL, "/")
	req, err := c.SignedRequest(ctx, http.MethodGet, base+"/manifest")
	if err != nil {
		return c.loadCachedManifest()
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		// Пытаемся загрузить из кэша при ошибке подключения
		if cached, cacheErr := c.loadCachedManifest(); cacheErr == nil {
			return cached, nil
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Пытаемся загрузить из кэша при ошибке сервера
		if cached, cacheErr := c.loadCachedManifest(); cacheErr == nil {
			// Кэш загружен успешно - возвращаем без ошибки
			return cached, nil
		}
		// Кэш недоступен - возвращаем ошибку (будет использован fallback на конфиг в launcher.go)
		return nil, errors.New("manifest unavailable: server error " + resp.Status)
	}
	
	// Читаем ответ
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.loadCachedManifest()
	}
	
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return c.loadCachedManifest()
	}
	
	// Сохраняем манифест в кэш при успешной загрузке
	_ = c.saveCachedManifest(body)
	
	return &manifest, nil
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
	path, err := c.manifestCachePath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
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
