package app

import (
	"context"

	"shinecore/internal/models/account"
)

// App is a minimal stub to allow the Wails window to start without backend logic.
type App struct{}

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

func New() *App {
	return &App{}
}

func (a *App) Startup(ctx context.Context) {}

func (a *App) DomReady(ctx context.Context) {}

// Frontend stubs to keep the UI running without backend logic.
func (a *App) GetUserChannels() []string {
	return []string{"stable"}
}

func (a *App) GetState() *State {
	return &State{
		Channel: "stable",
		Dependencies: &Dependencies{
			Game: nil,
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
	return false
}

func (a *App) GetAccount() *account.Account {
	return nil
}

func (a *App) IsLoggedIn() bool {
	return false
}

func (a *App) Logout() {}

func (a *App) SetUserProfile(uuid string) {}

func (a *App) LaunchGame(params LaunchParams) {}

func (a *App) GetPlayerName() string {
	return ""
}

func (a *App) IsGameInstalled() bool {
	return false
}

func (a *App) InstallGame() {}

func (a *App) OpenGameDirectory() {}

func (a *App) StartServer() {}

func (a *App) StopServer() {}

func (a *App) IsServerRunning() bool {
	return false
}
