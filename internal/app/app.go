package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"shinecore/internal/launcher"
	"shinecore/internal/launcher/config"
	"shinecore/internal/launcher/server"
	"shinecore/internal/system"
	"shinecore/internal/models/account"
)

// App handles the launcher backend logic.
type App struct {
	ctx      context.Context
	launcher *launcher.Launcher
}

type DependencyVersion struct {
	Version string `json:"version"`
}

type Dependencies struct {
	Game *DependencyVersion `json:"game"`
	LKG  *DependencyVersion `json:"lkg"`
}

type State struct {
	Channel      string        `json:"channel"`
	Dependencies *Dependencies `json:"dependencies"`
}

type LaunchParams struct {
	PlayerName string `json:"playerName"`
}

type MemorySettings struct {
	CurrentMB int `json:"currentMB"`
	MinMB     int `json:"minMB"`
	MaxMB     int `json:"maxMB"`
}

func New() *App {
	return &App{
		launcher: &launcher.Launcher{},
	}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	go func() {
		timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.launcher.RefreshFromServer(timeout)
	}()
}

func (a *App) DomReady(ctx context.Context) {}

// Frontend stubs to keep the UI running without backend logic.
func (a *App) GetUserChannels() []string {
	return []string{"stable"}
}

func (a *App) GetState() *State {
	cfg, _ := a.launcher.LoadConfig()
	game := ""
	if cfg != nil {
		game = cfg.GameVersion
		if serverCfg, err := config.LoadServer(""); err == nil {
			client := &server.Client{BaseURL: serverCfg.ServerBaseURL, Secret: serverCfg.ServerSecret}
			if manifest, err := client.FetchManifest(context.Background()); err == nil {
				if manifest.Version != "" {
					game = manifest.Version
				} else if manifest.Dependencies.GameVersion != "" {
					game = manifest.Dependencies.GameVersion
				}
			}
		}
	}
	return &State{
		Channel: "stable",
		Dependencies: &Dependencies{
			Game: &DependencyVersion{Version: game},
			LKG:  nil,
		},
	}
}

func (a *App) SetChannel(channel string) {}

func (a *App) CheckForUpdates(force bool) int {
	return 0
}

func (a *App) RefreshNewsFeed() {}

func (a *App) CheckNetworkMode(force bool, reason string) bool {
	return true
}

func (a *App) GetAccount() *account.Account {
	userProfile, err := config.LoadProfile("")
	if err != nil {
		return nil
	}
	if userProfile.PlayerName == "" {
		return nil
	}
	if userProfile.PlayerUUID == "" {
		userProfile.PlayerUUID = launcher.OfflineUUID(userProfile.PlayerName)
		_ = userProfile.Save("")
	}
	profile := account.Profile{
		UUID: userProfile.PlayerUUID,
		Name: userProfile.PlayerName,
	}
	return &account.Account{
		Profiles:        []account.Profile{profile},
		SelectedProfile: userProfile.PlayerUUID,
	}
}

func (a *App) IsLoggedIn() bool {
	profile, err := config.LoadProfile("")
	if err != nil {
		return false
	}
	return profile.PlayerName != ""
}

func (a *App) Logout() {}

func (a *App) SetUserProfile(uuid string) error {
	profile, err := config.LoadProfile("")
	if err != nil {
		return err
	}
	profile.PlayerUUID = uuid
	return profile.Save("")
}

func (a *App) LaunchGame(params LaunchParams) error {
	if a.ctx == nil {
		return errors.New("app not ready")
	}
	if enabled := a.GetConsoleEnabled(); enabled {
		_ = a.OpenConsoleWindow()
	}
	{
		err := a.prepareForLaunch(func(evt launcher.ProgressEvent) {
			if a.ctx == nil {
				return
			}
			payload := map[string]any{
				"progress": evt.Progress * 100,
				"step":     evt.Step,
				"done":     evt.Done,
				"total":    evt.Total,
			}
			runtime.EventsEmit(a.ctx, "sync:progress", payload)
		})
		if err != nil {
			runtime.EventsEmit(a.ctx, "sync:error", err.Error())
			return err
		}
		runtime.EventsEmit(a.ctx, "sync:complete")
	}
	if err := a.launcher.Launch(a.ctx, params.PlayerName); err != nil {
		runtime.EventsEmit(a.ctx, "launch:error", err.Error())
		return err
	}
	return nil
}

func (a *App) GetPlayerName() string {
	profile, err := config.LoadProfile("")
	if err != nil {
		return ""
	}
	return profile.PlayerName
}

func (a *App) GetConsoleEnabled() bool {
	cfg, err := a.launcher.LoadConfig()
	if err != nil || cfg == nil {
		return false
	}
	return cfg.ConsoleEnabled
}

func (a *App) SetConsoleEnabled(enabled bool) error {
	cfg, err := a.launcher.LoadConfig()
	if err != nil {
		return err
	}
	cfg.ConsoleEnabled = enabled
	return cfg.Save(a.launcher.ConfigPath)
}

func (a *App) GetLogTail(lines int) string {
	if lines <= 0 {
		lines = 200
	}
	path, err := logFilePath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	parts := strings.Split(content, "\n")
	if len(parts) <= lines {
		return content
	}
	return strings.Join(parts[len(parts)-lines:], "\n")
}

func (a *App) OpenConsoleWindow() error {
	path, err := logFilePath()
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	cmd := exec.Command("cmd", "/c", "start", "ShineCore Console", "powershell", "-NoProfile", "-Command", "Get-Content -Path \""+path+"\" -Wait")
	return cmd.Start()
}

func (a *App) GetMemorySettings() *MemorySettings {
	cfg, _ := a.launcher.LoadConfig()
	current := 4096
	if cfg != nil && cfg.MemoryMB > 0 {
		current = cfg.MemoryMB
	}
	max := system.SystemMemoryMB()
	if max < 512 {
		max = 512
	}
	if current < 512 {
		current = 512
	}
	if current > max {
		current = max
	}
	return &MemorySettings{
		CurrentMB: current,
		MinMB:     512,
		MaxMB:     max,
	}
}

func (a *App) SetMemoryMB(value int) error {
	cfg, err := a.launcher.LoadConfig()
	if err != nil {
		return err
	}
	if value < 512 {
		value = 512
	}
	max := system.SystemMemoryMB()
	if max > 0 && value > max {
		value = max
	}
	cfg.MemoryMB = value
	return cfg.Save(a.launcher.ConfigPath)
}

func (a *App) IsGameInstalled() bool {
	ok, err := a.launcher.IsInstalled()
	if err != nil {
		return false
	}
	return ok
}

func (a *App) InstallGame() error {
	if a.ctx == nil {
		return errors.New("app not ready")
	}
	_, err := a.launcher.Install(a.ctx, func(evt launcher.ProgressEvent) {
		if a.ctx == nil {
			return
		}
		payload := map[string]any{
			"progress": evt.Progress * 100,
			"step":     evt.Step,
			"done":     evt.Done,
			"total":    evt.Total,
		}
		runtime.EventsEmit(a.ctx, "install:progress", payload)
	})
	if err != nil {
		runtime.EventsEmit(a.ctx, "install:error", err.Error())
		return err
	}
	runtime.EventsEmit(a.ctx, "install:complete")
	return nil
}

func (a *App) prepareForLaunch(onProgress func(launcher.ProgressEvent)) error {
	if a.ctx == nil {
		return errors.New("app not ready")
	}
	return a.launcher.PrepareForLaunch(a.ctx, onProgress)
}

func (a *App) OpenGameDirectory() {
	if a.ctx == nil {
		return
	}
	cfg, err := a.launcher.LoadConfig()
	if err != nil {
		runtime.EventsEmit(a.ctx, "open:error", err.Error())
		return
	}
	openDirectory(cfg.InstallDir)
}

func (a *App) GetInstallDir() string {
	cfg, err := a.launcher.LoadConfig()
	if err != nil {
		return ""
	}
	return cfg.InstallDir
}

func (a *App) SetInstallDir(path string) error {
	if path == "" {
		return errors.New("install dir required")
	}
	cfg, err := a.launcher.LoadConfig()
	if err != nil {
		return err
	}
	cfg.InstallDir = path
	return cfg.Save(a.launcher.ConfigPath)
}

func (a *App) SelectInstallDir() (string, error) {
	if a.ctx == nil {
		return "", errors.New("app not ready")
	}
	cfg, _ := a.launcher.LoadConfig()
	defaultDir := ""
	if cfg != nil {
		defaultDir = cfg.InstallDir
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		DefaultDirectory: defaultDir,
	})
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", nil
	}
	if err := a.SetInstallDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func openDirectory(path string) {
	if path == "" {
		return
	}
	_ = exec.Command("explorer", path).Start()
}

func logFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "shinecore", "launcher.log"), nil
}

func (a *App) StartServer() {}

func (a *App) StopServer() {}

func (a *App) IsServerRunning() bool {
	return false
}
