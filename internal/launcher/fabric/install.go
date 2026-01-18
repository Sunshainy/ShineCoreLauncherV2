package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"shinecore/internal/launcher/download"
	"shinecore/internal/launcher/mojang"
)

const (
	metaBase = "https://meta.fabricmc.net"
	// Резервные зеркала для Fabric API (fallback URLs)
	metaMirrors = "https://maven.fabricmc.net"
)

func EnsureInstalled(ctx context.Context, baseDir, gameVersion, loaderVersion string, client *http.Client) (string, string, error) {
	if strings.TrimSpace(gameVersion) == "" {
		return "", "", errors.New("game version is required")
	}
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(loaderVersion) == "" {
		latest, err := fetchLatestLoader(ctx, client)
		if err != nil {
			return "", "", err
		}
		loaderVersion = latest
	}

	// Пытаемся загрузить профиль с основного сервера, при неудаче - с зеркала
	profileURLs := []string{
		fmt.Sprintf("%s/v2/versions/loader/%s/%s/profile/json", metaBase, gameVersion, loaderVersion),
		fmt.Sprintf("%s/v2/versions/loader/%s/%s/profile/json", metaMirrors, gameVersion, loaderVersion),
	}
	
	var resp *http.Response
	var lastErr error
	for _, url := range profileURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err = client.Do(req)
		if err != nil {
			lastErr = err
			if resp != nil {
				resp.Body.Close()
			}
			resp = nil
			continue
		}
		if resp.StatusCode == http.StatusOK {
			break // Успешно загрузили
		}
		status := resp.Status
		resp.Body.Close()
		resp = nil
		lastErr = errors.New("fabric profile error: " + status)
	}
	
	if resp == nil {
		if lastErr != nil {
			return "", "", fmt.Errorf("fabric profile download failed from all mirrors: %w", lastErr)
		}
		return "", "", errors.New("fabric profile download failed from all mirrors")
	}
	defer resp.Body.Close()
	var meta mojang.VersionMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return "", "", err
	}
	versionDir := filepath.Join(baseDir, "versions", meta.ID)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return "", "", err
	}
	metaPath := filepath.Join(versionDir, meta.ID+".json")
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(metaPath, data, 0o644); err != nil {
		return "", "", err
	}

	if err := downloadProfileLibraries(ctx, client, baseDir, meta); err != nil {
		return "", "", err
	}

	return meta.ID, loaderVersion, nil
}

func fetchLatestLoader(ctx context.Context, client *http.Client) (string, error) {
	// Пытаемся загрузить список версий с основного сервера, при неудаче - с зеркала
	urls := []string{
		metaBase + "/v2/versions/loader",
		metaMirrors + "/v2/versions/loader",
	}
	
	var resp *http.Response
	var lastErr error
	for _, url := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err = client.Do(req)
		if err != nil {
			lastErr = err
			if resp != nil {
				resp.Body.Close()
			}
			resp = nil
			continue
		}
		if resp.StatusCode == http.StatusOK {
			break // Успешно загрузили
		}
		status := resp.Status
		resp.Body.Close()
		resp = nil
		lastErr = errors.New("fabric loader list error: " + status)
	}
	
	if resp == nil {
		if lastErr != nil {
			return "", fmt.Errorf("fabric loader list download failed from all mirrors: %w", lastErr)
		}
		return "", errors.New("fabric loader list download failed from all mirrors")
	}
	defer resp.Body.Close()
	var versions []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return "", err
	}
	for _, v := range versions {
		if v.Stable {
			return v.Version, nil
		}
	}
	if len(versions) > 0 {
		return versions[0].Version, nil
	}
	return "", errors.New("no fabric loader versions")
}

func downloadProfileLibraries(ctx context.Context, client *http.Client, baseDir string, meta mojang.VersionMetadata) error {
	for _, lib := range meta.Libraries {
		if !allowLibrary(lib.Rules) {
			continue
		}
		if lib.Downloads != nil && lib.Downloads.Artifact != nil {
			if err := downloadLibrary(ctx, client, baseDir, lib.Downloads.Artifact); err != nil {
				return err
			}
		} else {
			if err := downloadLibraryByName(ctx, client, baseDir, lib.Name, lib.URL); err != nil {
				return err
			}
		}
	}
	return nil
}

func downloadLibrary(ctx context.Context, client *http.Client, baseDir string, artifact *mojang.LibraryArtifact) error {
	dst := filepath.Join(baseDir, "libraries", filepath.FromSlash(artifact.Path))
	return downloadEnsureFile(ctx, client, artifact.URL, dst, artifact.Size)
}

func downloadLibraryByName(ctx context.Context, client *http.Client, baseDir, name, baseURL string) error {
	path := libraryPath(name)
	if path == "" {
		return nil
	}
	url := "https://libraries.minecraft.net/" + path
	if strings.TrimSpace(baseURL) != "" {
		url = strings.TrimRight(baseURL, "/") + "/" + path
	}
	dst := filepath.Join(baseDir, "libraries", filepath.FromSlash(path))
	return downloadEnsureFile(ctx, client, url, dst, 0)
}

func libraryPath(name string) string {
	parts := strings.Split(name, ":")
	if len(parts) < 3 {
		return ""
	}
	group := strings.ReplaceAll(parts[0], ".", "/")
	artifact := parts[1]
	version := parts[2]
	classifier := ""
	ext := "jar"
	if len(parts) >= 4 {
		classifier = parts[3]
	}
	if strings.Contains(version, "@") {
		split := strings.Split(version, "@")
		version = split[0]
		ext = split[1]
	}
	file := artifact + "-" + version
	if classifier != "" {
		file += "-" + classifier
	}
	file += "." + ext
	return group + "/" + artifact + "/" + version + "/" + file
}

func allowLibrary(rules []mojang.Rule) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, rule := range rules {
		match := true
		if rule.OS != nil && rule.OS.Name != "" {
			match = rule.OS.Name == osName()
		}
		if rule.Action == "allow" && match {
			allowed = true
		}
		if rule.Action == "disallow" && match {
			allowed = false
		}
	}
	return allowed
}

func osName() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "osx"
	default:
		return "linux"
	}
}

func downloadEnsureFile(ctx context.Context, client *http.Client, url, dst string, size int64) error {
	return download.EnsureFile(ctx, client, url, dst, size, "", nil)
}
