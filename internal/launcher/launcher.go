package launcher

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"shinecore/internal/launcher/archive"
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

var mcVersionRe = regexp.MustCompile(`^1\.(\d+)(?:\.(\d+))?`)

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
	
	// Сохраняем старые значения перед применением манифеста
	oldGameVersion := cfg.GameVersion
	oldLoader := cfg.Loader
	
	manifest, err := srv.FetchManifest(ctx)
	if err != nil {
		// Fallback: Если манифест недоступен - используем сохранённый конфиг
		if strings.TrimSpace(cfg.GameVersion) == "" {
			slog.Error("launcher: manifest unavailable and config incomplete", "error", err)
			return nil, errors.New("manifest unavailable and config incomplete: " + err.Error())
		}
		// Используем сохранённый конфиг для продолжения установки/запуска
		slog.Info("launcher: manifest unavailable, using saved config", 
			"version", cfg.GameVersion, "loader", cfg.Loader, 
			"note", "server offline or manifest cache missing - using local config")
		manifest = nil // Явно указываем, что манифеста нет
	} else {
		slog.Info("launcher: manifest fetched", "mods", len(manifest.Packages.Mods))
		applyManifest(cfg, manifest)
	}
	
	// Проверяем, изменились ли версия игры или загрузчик
	versionChanged := oldGameVersion != "" && oldGameVersion != cfg.GameVersion
	loaderChanged := oldLoader != cfg.Loader
	
	if versionChanged || loaderChanged {
		slog.Info("launcher: version or loader changed", 
			"old_version", oldGameVersion, "new_version", cfg.GameVersion,
			"old_loader", oldLoader, "new_loader", cfg.Loader)
		if err := cleanInstallDir(cfg.InstallDir); err != nil {
			slog.Error("launcher: clean install dir failed", "error", err)
			return nil, err
		}
	}
	
	if err := cfg.Save(l.ConfigPath); err != nil {
		slog.Error("launcher: save config failed", "error", err)
		return nil, err
	}

	if err := os.MkdirAll(cfg.InstallDir, 0o755); err != nil {
		slog.Error("launcher: create install dir failed", "error", err)
		return nil, err
	}

	tracker := newProgressTracker(onProgress)

	// Синхронизация модов только если манифест доступен
	if manifest != nil {
		if err := syncModsStrict(ctx, client, srv, cfg.InstallDir, manifest, tracker); err != nil {
			slog.Error("launcher: sync packages failed", "error", err)
			return nil, err
		}
	} else {
		slog.Info("launcher: skipping mods sync (manifest unavailable)")
	}

	requiredJava := resolveRequiredJava(manifest, cfg.GameVersion)
	
	// Сначала проверяем локально установленную Java
	javaPath := findInstalledJava(cfg.InstallDir, requiredJava)
	
	// Если Java не найдена локально и манифест доступен - пытаемся загрузить
	if javaPath == "" && manifest != nil {
		var err error
		javaPath, err = ensureJava(ctx, client, srv, cfg.InstallDir, manifest, requiredJava)
		if err != nil {
			slog.Warn("launcher: ensure java failed", "error", err)
			// Не блокируем установку, если Java можно будет найти позже
		}
	}
	
	// Повторная проверка локальной Java на случай, если ensureJava не сработал
	if javaPath == "" {
		javaPath = findInstalledJava(cfg.InstallDir, requiredJava)
	}

	_, err = mojang.EnsureInstalled(ctx, mojang.InstallRequest{
		BaseDir:      cfg.InstallDir,
		Version:      cfg.GameVersion,
		Client:       client,
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

	requiredJava := resolveRequiredJava(nil, cfg.GameVersion)
	slog.Info("launcher: checking java", "required", requiredJava, "install_dir", cfg.InstallDir)
	javaPath := findInstalledJava(cfg.InstallDir, requiredJava)
	if javaPath == "" {
		slog.Error("launcher: java not found", "required", requiredJava, "search_dir", javaVersionDir(cfg.InstallDir, requiredJava))
		if requiredJava > 0 {
			return errors.New("java не установлена: нужна версия " + strconv.Itoa(requiredJava))
		}
		return errors.New("java не установлена (runtime not found)")
	}
	slog.Info("launcher: java found", "path", javaPath, "version", requiredJava)

	versionID := resolveVersionID(cfg)
	slog.Info("launcher: launching", "version", versionID, "memory_mb", cfg.MemoryMB, "java", javaPath)
	return launch.Launch(ctx, launch.LaunchRequest{
		BaseDir:  cfg.InstallDir,
		Version:  versionID,
		Player:   launch.PlayerInfo{Name: playerName, UUID: playerUUID},
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
		// Если манифест недоступен - просто пропускаем синхронизацию модов
		// Это не критично, игра может работать с уже установленными модами
		slog.Info("launcher: sync mods skipped - manifest unavailable", "error", err)
		return nil
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

func ensureJava(ctx context.Context, client *http.Client, srv *server.Client, baseDir string, manifest *server.Manifest, required int) (string, error) {
	if required <= 0 {
		return "", errors.New("java version not resolved")
	}
	if path := findInstalledJava(baseDir, required); path != "" {
		return path, nil
	}

	if manifest == nil {
		return "", errors.New("java url not provided: need Java " + strconv.Itoa(required))
	}
	downloadURL := javaURLForVersion(manifest.Dependencies.JavaURLs, required)
	if strings.TrimSpace(downloadURL) == "" {
		return "", errors.New("java url not set: need Java " + strconv.Itoa(required))
	}

	downloadDir := filepath.Join(javaBaseDir(baseDir), "downloads")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return "", err
	}
	archiveName := javaArchiveName(downloadURL, required)
	dst := filepath.Join(downloadDir, archiveName)

	req, err := buildJavaDownloadRequest(ctx, srv, downloadURL)
	if err != nil {
		return "", err
	}
	if err := download.EnsureFileWithRequest(ctx, client, req, dst, 0, "", nil); err != nil {
		return "", err
	}

	ext := strings.ToLower(filepath.Ext(archiveName))
	switch ext {
	case ".zip":
		targetDir := javaVersionDir(baseDir, required)
		_ = os.RemoveAll(targetDir)
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return "", err
		}
		if err := archive.ExtractZip(dst, targetDir); err != nil {
			return "", err
		}
		if path := findJavaInDirTree(targetDir, required); path != "" {
			return path, nil
		}
		return "", errors.New("java not found after extract: need Java " + strconv.Itoa(required))
	case ".msi", ".exe":
		if err := runJavaInstaller(ctx, dst); err != nil {
			return "", err
		}
		if path := findInstalledJava(baseDir, required); path != "" {
			return path, nil
		}
		return "", errors.New("java installed but not found in local runtime: provide zip for Java " + strconv.Itoa(required))
	default:
		return "", errors.New("unsupported java package: " + ext)
	}
}

func javaBaseDir(baseDir string) string {
	return filepath.Join(baseDir, "java")
}

func javaVersionDir(baseDir string, required int) string {
	return filepath.Join(javaBaseDir(baseDir), "java"+strconv.Itoa(required))
}

func javaArchiveName(raw string, required int) string {
	name := ""
	if parsed, err := url.Parse(raw); err == nil {
		name = path.Base(parsed.Path)
	}
	if name == "" || name == "." || name == "/" {
		return "java" + strconv.Itoa(required) + ".zip"
	}
	return name
}

func javaURLForVersion(urls server.JavaURLs, required int) string {
	switch required {
	case 8:
		return urls.Java8
	case 17:
		return urls.Java17
	case 21:
		return urls.Java21
	default:
		return ""
	}
}

func buildJavaDownloadRequest(ctx context.Context, srv *server.Client, raw string) (*http.Request, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return http.NewRequestWithContext(ctx, http.MethodGet, trimmed, nil)
	}
	if srv != nil {
		resolved := srv.ResolveURL(trimmed)
		return srv.SignedRequest(ctx, http.MethodGet, resolved)
	}
	return http.NewRequestWithContext(ctx, http.MethodGet, trimmed, nil)
}

func findInstalledJava(baseDir string, required int) string {
	root := javaBaseDir(baseDir)
	if required > 0 {
		targetDir := javaVersionDir(baseDir, required)
		slog.Debug("launcher: searching java", "dir", targetDir, "required", required)
		if path := findJavaInDirTree(targetDir, required); path != "" {
			return path
		}
		return ""
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := findJavaInDirTree(filepath.Join(root, entry.Name()), required)
		if path != "" {
			return path
		}
	}
	return ""
}

func findJavaInDirTree(dir string, required int) string {
	if dir == "" {
		return ""
	}
	if path := findJavaInDirWithMajor(dir, required); path != "" {
		return path
	}
	var foundJavaW string
	var foundJava string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		isJavaW := name == "javaw.exe"
		isJava := name == "java.exe"
		if !isJavaW && !isJava {
			return nil
		}
		if strings.ToLower(filepath.Base(filepath.Dir(path))) != "bin" {
			return nil
		}
		if required > 0 {
			major, err := java.GetJavaMajor(path)
			if err != nil || major != required {
				return nil
			}
		}
		if isJavaW {
			foundJavaW = path
		} else if foundJava == "" {
			foundJava = path
		}
		return nil
	})
	if foundJavaW != "" {
		return foundJavaW
	}
	if foundJava != "" {
		return foundJava
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

func findJavaInDirWithMajor(dir string, required int) string {
	if required <= 0 {
		return findJavaInDir(dir)
	}
	path := findJavaInDir(dir)
	if path == "" {
		return ""
	}
	major, err := java.GetJavaMajor(path)
	if err != nil || major != required {
		return ""
	}
	return path
}

func resolveRequiredJava(manifest *server.Manifest, gameVersion string) int {
	version := strings.TrimSpace(gameVersion)
	if manifest != nil && strings.TrimSpace(manifest.Dependencies.GameVersion) != "" {
		version = manifest.Dependencies.GameVersion
	}
	if required, ok := javaRequiredForMinecraft(version); ok {
		return required
	}
	return 0
}

func javaRequiredForMinecraft(version string) (int, bool) {
	match := mcVersionRe.FindStringSubmatch(strings.TrimSpace(version))
	if len(match) < 2 {
		return 0, false
	}
	minor, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	patch := 0
	if len(match) > 2 && match[2] != "" {
		if patch, err = strconv.Atoi(match[2]); err != nil {
			return 0, false
		}
	}
	switch {
	case minor <= 16:
		return 8, true
	case minor == 17:
		return 16, true
	case minor > 20:
		return 21, true
	case minor == 20 && patch >= 5:
		return 21, true
	case minor >= 18:
		return 17, true
	default:
		return 0, false
	}
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

func cleanInstallDir(baseDir string) error {
	if strings.TrimSpace(baseDir) == "" {
		return errors.New("install dir is empty")
	}
	
	// Папки и файлы, которые нужно удалить
	itemsToRemove := []string{
		"versions",
		"libraries",
		"mods",
		"assets",
		"bin",
		"logs",
		"saves",
		"resourcepacks",
		"shaderpacks",
		"config",
		"screenshots",
		"options.txt",
		"optionsof.txt",
		"usercache.json",
		"usernamecache.json",
	}
	
	javaDir := filepath.Join(baseDir, "java")
	
	for _, item := range itemsToRemove {
		path := filepath.Join(baseDir, item)
		if err := os.RemoveAll(path); err != nil {
			slog.Warn("launcher: failed to remove item", "path", path, "error", err)
		}
	}
	
	// Убеждаемся, что папка java осталась
	if err := os.MkdirAll(javaDir, 0o755); err != nil {
		return err
	}
	
	slog.Info("launcher: install dir cleaned", "base_dir", baseDir)
	return nil
}

func runJavaInstaller(ctx context.Context, installerPath string) error {
	ext := strings.ToLower(filepath.Ext(installerPath))
	var cmd *exec.Cmd
	switch ext {
	case ".msi":
		cmd = exec.CommandContext(ctx, "msiexec", "/i", installerPath, "/qn", "/norestart")
	case ".exe":
		cmd = exec.CommandContext(ctx, installerPath, "/qn", "/norestart")
	default:
		return errors.New("unsupported java installer: " + ext)
	}
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000, // CREATE_NO_WINDOW
		}
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New("java installer failed: " + string(output))
	}
	return nil
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
