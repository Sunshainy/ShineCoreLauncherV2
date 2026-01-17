package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultInstallDirName = "shinecore"
	configFileName        = "launcher.json"
	defaultServerBaseURL  = "https://api.be-sunshainy.ru"
)

type Config struct {
	InstallDir    string `json:"install_dir"`
	GameVersion   string `json:"game_version"`
	Loader        string `json:"loader"`         // fabric|forge|neoforge
	LoaderVersion string `json:"loader_version"` // optional for latest

	MemoryMB       int  `json:"memory_mb"`
	ConsoleEnabled bool `json:"console_enabled"`
}

func DefaultInstallDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, defaultInstallDirName), nil
}

func ConfigPath() (string, error) {
	base, err := DefaultInstallDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, configFileName), nil
}

func ServerConfigPath() (string, error) {
	base, err := DefaultInstallDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "server.json"), nil
}

func ProfilePath() (string, error) {
	base, err := DefaultInstallDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "profile.json"), nil
}

func Load(path string) (*Config, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = ConfigPath()
		if err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := &Config{}
			return cfg.applyDefaults()
		}
		return nil, err
	}

	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg.applyDefaults()
}

func (c *Config) Save(path string) error {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = ConfigPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}

func (c *Config) applyDefaults() (*Config, error) {
	if strings.TrimSpace(c.InstallDir) == "" {
		installDir, err := DefaultInstallDir()
		if err != nil {
			return nil, err
		}
		c.InstallDir = installDir
	}
	if c.MemoryMB <= 0 {
		c.MemoryMB = 4096
	}
	if c.MemoryMB < 512 {
		c.MemoryMB = 512
	}
	c.Loader = strings.ToLower(strings.TrimSpace(c.Loader))
	switch c.Loader {
	case "", "fabric", "forge", "neoforge":
	default:
		return nil, errors.New("unsupported loader: " + c.Loader)
	}
	return c, nil
}

type ServerConfig struct {
	ServerBaseURL string `json:"server_base_url"`
	ServerSecret  string `json:"server_secret"`
}

func LoadServer(path string) (*ServerConfig, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = ServerConfigPath()
		if err != nil {
			return nil, err
		}
	}
	cfg := &ServerConfig{
		ServerBaseURL: defaultServerBaseURL,
		ServerSecret:  "sun",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.ServerBaseURL = normalizeServerBaseURL(cfg.ServerBaseURL)
	if strings.TrimSpace(cfg.ServerBaseURL) == "" {
		cfg.ServerBaseURL = defaultServerBaseURL
	}
	return cfg, nil
}

func (c *ServerConfig) Save(path string) error {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = ServerConfigPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}

func normalizeServerBaseURL(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		return ""
	}
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		return base
	}
	return "https://" + base
}

type Profile struct {
	PlayerName string `json:"player_name"`
	PlayerUUID string `json:"player_uuid"`
}

func LoadProfile(path string) (*Profile, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = ProfilePath()
		if err != nil {
			return nil, err
		}
	}
	profile := &Profile{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return profile, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, profile); err != nil {
		return nil, err
	}
	return profile, nil
}

func (p *Profile) Save(path string) error {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = ProfilePath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}
