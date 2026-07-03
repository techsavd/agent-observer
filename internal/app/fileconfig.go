package app

import (
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// fileConfig is the optional ~/.config/agent-observer/config.toml.
// Precedence: flags > environment > config file > built-in defaults.
type fileConfig struct {
	Providers    string `toml:"providers"`
	ClaudeDir    string `toml:"claude_dir"`
	CodexDir     string `toml:"codex_dir"`
	CursorDir    string `toml:"cursor_dir"`
	PluginsDir   string `toml:"plugins_dir"`
	PollInterval string `toml:"poll_interval"`
	NoWatch      bool   `toml:"no_watch"`
	Shell        bool   `toml:"shell"`
	Act          bool   `toml:"act"`
	Redact       bool   `toml:"redact"`
	LogFile      string `toml:"log_file"`
	LogLevel     string `toml:"log_level"`
}

func defaultConfigPath() string {
	if path := os.Getenv("AGENT_OBSERVER_CONFIG"); path != "" {
		return path
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "agent-observer", "config.toml")
}

// loadFileConfig reads the config file if present. A missing file is normal;
// a malformed one is reported so it is not silently ignored.
func loadFileConfig(path string) (fileConfig, error) {
	var cfg fileConfig
	payload, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := toml.Unmarshal(payload, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c fileConfig) pollIntervalOr(fallback time.Duration) time.Duration {
	if c.PollInterval == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(c.PollInterval)
	if err != nil {
		return fallback
	}
	return parsed
}
