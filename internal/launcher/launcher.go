package launcher

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"shinecore/internal/launcher/config"
	"shinecore/internal/launcher/download"
	"shinecore/internal/launcher/fabric"
	"shinecore/internal/launcher/forge"
	"shinecore/internal/launcher/java"
	"shinecore/internal/launcher/launch"
	"shinecore/internal/launcher/mojang"
	"shinecore/internal/launcher/server"
)

type ProgressEvent struct {
	Step     string
	Done     int
	Total    int
	Progress float64
}

type Launcher struct {
	ConfigPath string
}

func (l *Launcher) LoadConfig() (*config.Config, error) {
	return config.Load(l.ConfigPath)
}

func (l *Launcher) Install(ctx context.Context, onProgress func(ProgressEvent)) (*config.Config, error) {
	cfg, err := l.LoadConfig()
	if err != nil {
		return nil, err
	}
	serverCfg, err := config.LoadServer("")
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	srv := &server.Client{BaseURL: serverCfg.ServerBaseURL, Secret: serverCfg.ServerSecret, Client: client}
	slog.Info("launcher: fetch manifest", "server", serverCfg.ServerBaseURL)
	manifest, err := srv.FetchManifest(ctx)
	if err != nil {
		slog.Error("launcher: manifest failed", "error", err)
		return nil, err
	}
	slog.Info("launcher: manifest fetched", "mods", len(manifest.Packages.Mods), "javas", len(manifest.Packages.Javas))
	applyManifest(cfg, manifest)
	if err := cfg.Save(l.ConfigPath); err != nil {
		slog.Error("launcher: save config failed", "error", err)
		return nil, err
	}

	if err := os.MkdirAll(cfg.InstallDir, 0o755); err != nil {
		slog.Error("launcher: create install dir failed", "error", err)
		return nil, err
	}

	tracker := newProgressTracker(onProgress)

	if err := syncModsStrict(ctx, client, srv, cfg.InstallDir, manifest, tracker); err != nil {
		slog.Error("launcher: sync packages failed", "error", err)
		return nil, err
	}

	javaPath, err := ensureJava(ctx, client, srv, cfg.InstallDir, manifest, cfg.JavaPackage)
	if err != nil {
		slog.Error("launcher: ensure java failed", "error", err)
		return nil, err
	}
	if javaPath == "" {
		javaPath = findInstalledJava(cfg.InstallDir, cfg.JavaPackage)
	}

	_, err = mojang.EnsureInstalled(ctx, mojang.InstallRequest{
		BaseDir: cfg.InstallDir,
		Version: cfg.GameVersion,
		Client:  client,
		AssetWorkers: 16,
		OnProgress: func(step string, done, total int) {
			tracker.Update(step, done, total)
		},
	})
	if err != nil {
		slog.Error("launcher: mojang install failed", "error", err)
		return nil, err
	}

	var versionID string
	switch cfg.Loader {
	case "":
		cfg.LoaderVersion = ""
		_ = cfg.Save(l.ConfigPath)
		versionID = cfg.GameVersion
	case "fabric":
		var loaderVersion string
		versionID, loaderVersion, err = fabric.EnsureInstalled(ctx, cfg.InstallDir, cfg.GameVersion, cfg.LoaderVersion, client)
		if err != nil {
			slog.Error("launcher: fabric install failed", "error", err)
			return nil, err
		}
		if loaderVersion != "" && loaderVersion != cfg.LoaderVersion {
			cfg.LoaderVersion = loaderVersion
			_ = cfg.Save(l.ConfigPath)
		}
		if err := mojang.EnsureLibrariesForVersion(ctx, cfg.InstallDir, versionID, client); err != nil {
			return nil, err
		}
	case "forge":
		if javaPath == "" {
			return nil, errors.New("java не установлена (runtime not found)")
		}
		versionID, err = forge.EnsureInstalled(ctx, forge.InstallRequest{
			BaseDir:       cfg.InstallDir,
			GameVersion:   cfg.GameVersion,
			LoaderKind:    forge.LoaderForge,
			LoaderVersion: cfg.LoaderVersion,
			JavaPath:      javaPath,
			Client:        client,
		})
		if err != nil {
			slog.Error("launcher: forge install failed", "error", err)
			return nil, err
		}
		if err := mojang.EnsureLibrariesForVersion(ctx, cfg.InstallDir, versionID, client); err != nil {
			return nil, err
		}
	case "neoforge":
		if javaPath == "" {
			return nil, errors.New("java не установлена (runtime not found)")
		}
		versionID, err = forge.EnsureInstalled(ctx, forge.InstallRequest{
			BaseDir:       cfg.InstallDir,
			GameVersion:   cfg.GameVersion,
			LoaderKind:    forge.LoaderNeoForge,
			LoaderVersion: cfg.LoaderVersion,
			JavaPath:      javaPath,
			Client:        client,
		})
		if err != nil {
			slog.Error("launcher: neoforge install failed", "error", err)
			return nil, err
		}
		if err := mojang.EnsureLibrariesForVersion(ctx, cfg.InstallDir, versionID, client); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unknown loader: " + cfg.Loader)
	}

	nativesCount, err := launch.PrepareNatives(cfg.InstallDir, versionID)
	if err != nil {
		slog.Error("launcher: natives prepare failed", "error", err)
		return nil, err
	}
	if nativesCount > 0 {
		tracker.SetTotal("natives", nativesCount)
		tracker.Update("natives", nativesCount, nativesCount)
	}

	return cfg, nil
}

func (l *Launcher) Launch(ctx context.Context, playerName string) error {
	cfg, err := l.LoadConfig()
	if err != nil {
		return err
	}
	profile, err := config.LoadProfile("")
	if err != nil {
		return err
	}
	if strings.TrimSpace(playerName) == "" {
		playerName = profile.PlayerName
	}
	if strings.TrimSpace(playerName) == "" {
		return errors.New("player name required")
	}
	playerUUID := profile.PlayerUUID
	if strings.TrimSpace(playerUUID) == "" || profile.PlayerName != playerName {
		playerUUID = OfflineUUID(playerName)
		profile.PlayerName = playerName
		profile.PlayerUUID = playerUUID
		_ = profile.Save("")
	}

	javaPath := findInstalledJava(cfg.InstallDir, cfg.JavaPackage)
	if javaPath == "" {
		return errors.New("java не установлена (runtime not found)")
	}

	versionID := resolveVersionID(cfg)
	slog.Info("launcher: launching", "version", versionID, "memory_mb", cfg.MemoryMB)
	return launch.Launch(ctx, launch.LaunchRequest{
		BaseDir: cfg.InstallDir,
		Version: versionID,
		Player:  launch.PlayerInfo{Name: playerName, UUID: playerUUID},
		JavaPath: javaPath,
		MemoryMB: cfg.MemoryMB,
	})
}

func (l *Launcher) SyncMods(ctx context.Context, onProgress func(ProgressEvent)) error {
	cfg, err := l.LoadConfig()
	if err != nil {
		return err
	}
	serverCfg, err := config.LoadServer("")
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	srv := &server.Client{BaseURL: serverCfg.ServerBaseURL, Secret: serverCfg.ServerSecret, Client: client}
	slog.Info("launcher: sync mods start", "server", serverCfg.ServerBaseURL)
	manifest, err := srv.FetchManifest(ctx)
	if err != nil {
		return err
	}
	slog.Info("launcher: manifest fetched for sync", "mods", len(manifest.Packages.Mods))
	tracker := newProgressTracker(onProgress)
	if err := syncModsStrict(ctx, client, srv, cfg.InstallDir, manifest, tracker); err != nil {
		return err
	}
	slog.Info("launcher: sync mods complete")
	return nil
}

func (l *Launcher) IsInstalled() (bool, error) {
	cfg, err := l.LoadConfig()
	if err != nil {
		return false, err
	}
	versionID := resolveVersionID(cfg)
	metaPath := filepath.Join(cfg.InstallDir, "versions", versionID, versionID+".json")
	_, err = os.Stat(metaPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func applyManifest(cfg *config.Config, manifest *server.Manifest) {
	if manifest == nil {
		return
	}
	if manifest.Dependencies.GameVersion != "" {
		cfg.GameVersion = manifest.Dependencies.GameVersion
	}
	if manifest.Dependencies.Loader == "" {
		cfg.Loader = ""
		cfg.LoaderVersion = ""
	} else {
		cfg.Loader = manifest.Dependencies.Loader
		if manifest.Dependencies.LoaderVersion != "" {
			cfg.LoaderVersion = manifest.Dependencies.LoaderVersion
		}
	}
	if manifest.Dependencies.JavaPackage != "" {
		cfg.JavaPackage = manifest.Dependencies.JavaPackage
	}
}

func (l *Launcher) RefreshFromServer(ctx context.Context) (*config.Config, error) {
	cfg, err := l.LoadConfig()
	if err != nil {
		return nil, err
	}
	serverCfg, err := config.LoadServer("")
	if err != nil {
		return cfg, err
	}
	client := &http.Client{}
	srv := &server.Client{BaseURL: serverCfg.ServerBaseURL, Secret: serverCfg.ServerSecret, Client: client}
	manifest, err := srv.FetchManifest(ctx)
	if err != nil {
		return cfg, err
	}
	applyManifest(cfg, manifest)
	if err := cfg.Save(l.ConfigPath); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func syncModsStrict(ctx context.Context, client *http.Client, srv *server.Client, baseDir string, manifest *server.Manifest, tracker *progressTracker) error {
	if manifest == nil {
		return nil
	}
	if tracker == nil {
		tracker = newProgressTracker(nil)
	}
	modsDir := filepath.Join(baseDir, "mods")
	_ = os.MkdirAll(modsDir, 0o755)

	expected := make(map[string]server.FilePackage)
	for _, mod := range manifest.Packages.Mods {
		if mod.Path == "" || mod.URL == "" {
			continue
		}
		key := modKey(mod.Path)
		expected[key] = mod
	}

	local, err := listLocalMods(modsDir)
	if err != nil {
		return err
	}

	extras := make([]string, 0)
	for key, fullPath := range local {
		if _, ok := expected[key]; !ok {
			extras = append(extras, fullPath)
		}
	}

	tracker.SetTotal("mods", len(expected)+len(extras))
	slog.Info("mods: sync start", "expected", len(expected), "extras", len(extras))

	downloaded := 0
	for _, mod := range expected {
		dst := filepath.Join(modsDir, filepath.FromSlash(mod.Path))
		url := srv.ResolveURL(mod.URL)
		req, err := srv.SignedRequest(ctx, http.MethodGet, url)
		if err != nil {
			return err
		}
		if err := download.EnsureFileWithRequest(ctx, client, req, dst, mod.Size, mod.Sha256, nil); err != nil {
			return err
		}
		downloaded++
		tracker.Increment("mods")
	}

	removed := 0
	for _, fullPath := range extras {
		_ = os.Remove(fullPath)
		removed++
		tracker.Increment("mods")
	}

	slog.Info("mods: sync complete", "downloaded", downloaded, "removed", removed)
	return nil
}

func ensureJava(ctx context.Context, client *http.Client, srv *server.Client, baseDir string, manifest *server.Manifest, desired string) (string, error) {
	if manifest == nil {
		return "", nil
	}

	required := manifest.Dependencies.JavaVersion
	systemPath := java.FindSystemJava()
	if systemPath != "" {
		if required <= 0 {
			return systemPath, nil
		}
		if major, err := java.GetJavaMajor(systemPath); err == nil {
			if major == required {
				return systemPath, nil
			}
		}
	}

	if required <= 0 && len(manifest.Packages.Javas) == 0 {
		return "", nil
	}

	if len(manifest.Packages.Javas) == 0 {
		if systemPath != "" && required > 0 {
			return "", errors.New("java version mismatch: need " + strconv.Itoa(required))
		}
		return "", errors.New("java not found: install Java " + strconv.Itoa(required))
	}

	pkg := pickJavaPackage(manifest.Packages.Javas, desired)
	if pkg == nil {
		return "", errors.New("java package not found")
	}
	downloadURL := srv.ResolveURL(pkg.URL)
	req, err := srv.SignedRequest(ctx, http.MethodGet, downloadURL)
	if err != nil {
		return "", err
	}
	installerDir := filepath.Join(baseDir, "java", "installer")
	if err := os.MkdirAll(installerDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(installerDir, pkg.Name)
	if pkg.Name == "" {
		dst = filepath.Join(installerDir, filepath.Base(pkg.Path))
	}
	if err := download.EnsureFileWithRequest(ctx, client, req, dst, pkg.Size, pkg.Sha256, nil); err != nil {
		return "", err
	}
	if err := runJavaInstaller(ctx, dst); err != nil {
		return "", err
	}
	systemPath = java.FindSystemJava()
	if systemPath == "" {
		return "", errors.New("java not found after install: install Java " + strconv.Itoa(required))
	}
	if required > 0 {
		if major, err := java.GetJavaMajor(systemPath); err == nil && major != required {
			return "", errors.New("java version mismatch: need " + strconv.Itoa(required))
		}
	}
	return systemPath, nil
}

func pickJavaPackage(list []server.FilePackage, desired string) *server.FilePackage {
	if desired != "" {
		for i := range list {
			if list[i].Name == desired || filepath.Base(list[i].Path) == desired {
				return &list[i]
			}
		}
	}
	if len(list) > 0 {
		return &list[0]
	}
	return nil
}

func findInstalledJava(baseDir, desired string) string {
	if desired != "" {
		dir := filepath.Join(baseDir, "runtime", "java", strings.TrimSuffix(filepath.Base(desired), filepath.Ext(desired)))
		if path := findJavaInDir(dir); path != "" {
			return path
		}
	}
	root := filepath.Join(baseDir, "runtime", "java")
	entries, err := os.ReadDir(root)
	if err != nil {
		// Fall back to system Java.
		if path := java.FindSystemJava(); path != "" {
			return path
		}
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := findJavaInDir(filepath.Join(root, entry.Name()))
		if path != "" {
			return path
		}
	}
	if path := findSystemJava(); path != "" {
		return path
	}
	return ""
}

func findJavaInDir(dir string) string {
	path := filepath.Join(dir, "bin", "javaw.exe")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	path = filepath.Join(dir, "bin", "java.exe")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

func findSystemJava() string {
	return java.FindSystemJava()
}

func resolveVersionID(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	switch cfg.Loader {
	case "fabric":
		if cfg.LoaderVersion != "" {
			return "fabric-loader-" + cfg.LoaderVersion + "-" + cfg.GameVersion
		}
	case "forge", "neoforge":
		if cfg.LoaderVersion != "" {
			return cfg.Loader + "-" + cfg.LoaderVersion
		}
	}
	return cfg.GameVersion
}

func runJavaInstaller(ctx context.Context, installerPath string) error {
	ext := strings.ToLower(filepath.Ext(installerPath))
	switch ext {
	case ".msi":
		cmd := exec.CommandContext(ctx, "msiexec", "/i", installerPath, "/qn", "/norestart")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return errors.New("java installer failed: " + string(output))
		}
		return nil
	case ".exe":
		cmd := exec.CommandContext(ctx, installerPath, "/qn", "/norestart")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return errors.New("java installer failed: " + string(output))
		}
		return nil
	default:
		return errors.New("unsupported java installer: " + ext)
	}
}

func modKey(path string) string {
	return strings.ToLower(filepath.ToSlash(path))
}

func listLocalMods(root string) (map[string]string, error) {
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[modKey(rel)] = path
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	return result, nil
}

type progressTracker struct {
	onProgress func(ProgressEvent)
	totals     map[string]int
	done       map[string]int
	mu         sync.Mutex
}

func newProgressTracker(onProgress func(ProgressEvent)) *progressTracker {
	return &progressTracker{
		onProgress: onProgress,
		totals:     map[string]int{},
		done:       map[string]int{},
	}
}

func (p *progressTracker) SetTotal(step string, total int) {
	if total < 0 {
		return
	}
	p.mu.Lock()
	p.totals[step] = total
	p.mu.Unlock()
	p.emit(step)
}

func (p *progressTracker) Update(step string, done, total int) {
	p.mu.Lock()
	if total >= 0 {
		p.totals[step] = total
	}
	if done >= 0 {
		p.done[step] = done
	}
	p.mu.Unlock()
	p.emit(step)
}

func (p *progressTracker) Increment(step string) {
	p.mu.Lock()
	p.done[step] = p.done[step] + 1
	p.mu.Unlock()
	p.emit(step)
}

func (p *progressTracker) emit(step string) {
	if p.onProgress == nil {
		return
	}
	p.mu.Lock()
	totalAll := 0
	doneAll := 0
	for key, total := range p.totals {
		if total < 0 {
			continue
		}
		totalAll += total
		doneAll += minInt(p.done[key], total)
	}
	p.mu.Unlock()
	progress := 0.0
	if totalAll > 0 {
		progress = float64(doneAll) / float64(totalAll)
	}
	p.onProgress(ProgressEvent{Step: step, Done: doneAll, Total: totalAll, Progress: progress})
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
