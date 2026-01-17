package mojang

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"shinecore/internal/launcher/download"
)

const (
	manifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	libBaseURL  = "https://libraries.minecraft.net/"
)

type InstallRequest struct {
	BaseDir   string
	Version   string
	Client    *http.Client
	OnProgress func(step string, done, total int)
	AssetWorkers int
}

func EnsureInstalled(ctx context.Context, req InstallRequest) (*VersionMetadata, error) {
	if strings.TrimSpace(req.Version) == "" {
		return nil, errors.New("game version is required")
	}
	client := req.Client
	if client == nil {
		client = http.DefaultClient
	}

	meta, err := fetchVersionMetadata(ctx, client, req.Version)
	if err != nil {
		return nil, err
	}

	versionDir := filepath.Join(req.BaseDir, "versions", meta.ID)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return nil, err
	}
	metaPath := filepath.Join(versionDir, meta.ID+".json")
	if err := writeJSON(metaPath, meta); err != nil {
		return nil, err
	}

	if err := ensureClientJar(ctx, client, req.BaseDir, meta); err != nil {
		return nil, err
	}
	if err := ensureLibraries(ctx, client, req.BaseDir, meta, req.OnProgress); err != nil {
		return nil, err
	}
	if err := ensureAssets(ctx, client, req.BaseDir, meta, req.OnProgress, req.AssetWorkers); err != nil {
		return nil, err
	}
	return meta, nil
}

func EnsureLibrariesForVersion(ctx context.Context, baseDir, version string, client *http.Client) error {
	if client == nil {
		client = http.DefaultClient
	}
	metaPath := filepath.Join(baseDir, "versions", version, version+".json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return err
	}
	var meta VersionMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return err
	}
	return ensureLibraries(ctx, client, baseDir, &meta, nil)
}

func fetchVersionMetadata(ctx context.Context, client *http.Client, version string) (*VersionMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("manifest request failed: " + resp.Status)
	}
	var manifest VersionManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}
	var target *ManifestVersion
	for i := range manifest.Versions {
		if manifest.Versions[i].ID == version {
			target = &manifest.Versions[i]
			break
		}
	}
	if target == nil {
		return nil, errors.New("version not found: " + version)
	}
	versionReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
	if err != nil {
		return nil, err
	}
	versionResp, err := client.Do(versionReq)
	if err != nil {
		return nil, err
	}
	defer versionResp.Body.Close()
	if versionResp.StatusCode != http.StatusOK {
		return nil, errors.New("version metadata failed: " + versionResp.Status)
	}
	var meta VersionMetadata
	if err := json.NewDecoder(versionResp.Body).Decode(&meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func ensureClientJar(ctx context.Context, client *http.Client, baseDir string, meta *VersionMetadata) error {
	downloadInfo := meta.Downloads.Client
	dst := filepath.Join(baseDir, "versions", meta.ID, meta.ID+".jar")
	return download.EnsureFile(ctx, client, downloadInfo.URL, dst, downloadInfo.Size, "", nil)
}

func ensureLibraries(ctx context.Context, client *http.Client, baseDir string, meta *VersionMetadata, onProgress func(step string, done, total int)) error {
	total := 0
	for _, lib := range meta.Libraries {
		if !allowLibrary(lib.Rules) {
			continue
		}
		total++
		if lib.Downloads != nil && lib.Downloads.Artifact != nil {
			total += len(resolveNatives(lib))
		}
	}
	done := 0
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
		done++
		if onProgress != nil {
			onProgress("libraries", done, total)
		}
		for _, native := range resolveNatives(lib) {
			if err := downloadLibrary(ctx, client, baseDir, &native); err != nil {
				return err
			}
			done++
			if onProgress != nil {
				onProgress("libraries", done, total)
			}
		}
	}
	return nil
}

func ensureAssets(ctx context.Context, client *http.Client, baseDir string, meta *VersionMetadata, onProgress func(step string, done, total int), workers int) error {
	indexPath := filepath.Join(baseDir, "assets", "indexes", meta.AssetIndex.ID+".json")
	if err := download.EnsureFile(ctx, client, meta.AssetIndex.URL, indexPath, meta.AssetIndex.Size, "", nil); err != nil {
		return err
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	var index AssetIndexFile
	if err := json.Unmarshal(data, &index); err != nil {
		return err
	}
	total := len(index.Objects)
	if total == 0 {
		return nil
	}
	if workers <= 0 {
		workers = 8
	}
	type assetJob struct {
		Hash string
		Size int64
	}
	jobs := make(chan assetJob)
	errCh := make(chan error, 1)
	doneCh := make(chan struct{})
	var doneCount int64

	for i := 0; i < workers; i++ {
		go func() {
			for job := range jobs {
				hash := job.Hash
				if len(hash) < 2 {
					continue
				}
				sub := hash[:2]
				dst := filepath.Join(baseDir, "assets", "objects", sub, hash)
				url := "https://resources.download.minecraft.net/" + sub + "/" + hash
				if err := download.EnsureFile(ctx, client, url, dst, job.Size, "", nil); err != nil {
					select {
					case errCh <- err:
					default:
					}
					continue
				}
				atomic.AddInt64(&doneCount, 1)
				if onProgress != nil {
					onProgress("assets", int(atomic.LoadInt64(&doneCount)), total)
				}
			}
			doneCh <- struct{}{}
		}()
	}

	for _, obj := range index.Objects {
		select {
		case err := <-errCh:
			return err
		default:
		}
		jobs <- assetJob{Hash: obj.Hash, Size: obj.Size}
	}
	close(jobs)

	for i := 0; i < workers; i++ {
		<-doneCh
	}
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func downloadLibrary(ctx context.Context, client *http.Client, baseDir string, artifact *LibraryArtifact) error {
	dst := filepath.Join(baseDir, "libraries", filepath.FromSlash(artifact.Path))
	return download.EnsureFile(ctx, client, artifact.URL, dst, artifact.Size, "", nil)
}

func downloadLibraryByName(ctx context.Context, client *http.Client, baseDir, name, baseURL string) error {
	path := libraryPath(name)
	url := libBaseURL + path
	if strings.TrimSpace(baseURL) != "" {
		url = strings.TrimRight(baseURL, "/") + "/" + path
	}
	dst := filepath.Join(baseDir, "libraries", filepath.FromSlash(path))
	return download.EnsureFile(ctx, client, url, dst, 0, "", nil)
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
		parts := strings.Split(version, "@")
		version = parts[0]
		ext = parts[1]
	}
	file := artifact + "-" + version
	if classifier != "" {
		file += "-" + classifier
	}
	file += "." + ext
	return group + "/" + artifact + "/" + version + "/" + file
}

func resolveNatives(lib Library) []LibraryArtifact {
	if len(lib.Natives) == 0 || lib.Downloads == nil || len(lib.Downloads.Classifiers) == 0 {
		return nil
	}
	key := "windows"
	if runtime.GOOS == "linux" {
		key = "linux"
	} else if runtime.GOOS == "darwin" {
		key = "osx"
	}
	classifier, ok := lib.Natives[key]
	if !ok {
		return nil
	}
	artifact, ok := lib.Downloads.Classifiers[classifier]
	if !ok {
		return nil
	}
	return []LibraryArtifact{artifact}
}

func allowLibrary(rules []Rule) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, rule := range rules {
		match := true
		if rule.OS != nil && rule.OS.Name != "" {
			target := osName()
			match = rule.OS.Name == target
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

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
