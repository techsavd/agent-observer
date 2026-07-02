package app

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRefreshInterval = time.Second
	minRefreshInterval     = 200 * time.Millisecond
	maxRefreshInterval     = time.Minute
)

type command string

const (
	commandDashboard command = "dashboard"
	commandWatch     command = "watch"
	commandDoctor    command = "doctor"
)

type options struct {
	command         command
	providersList   string
	claudeDir       string
	codexDir        string
	cursorDir       string
	pluginsDir      string
	tasksDir        string
	teamsDir        string
	maxFileSize     int64
	refreshInterval time.Duration
	debug           bool
	dumpJSON        bool
	dumpText        bool
	dumpDiagnostics bool
	showVersion     bool
	shell           bool
	noShell         bool
	shellFlagSeen   bool
	noShellFlagSeen bool
	redact          bool
	focus           string
	logFile         string
	logLevel        string
	telemetry       string
	telemetryURL    string
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func validateOptions(opts *options) error {
	if opts.shellFlagSeen && opts.noShellFlagSeen {
		return fmt.Errorf("--shell and --no-shell cannot be used together")
	}
	if opts.shell && opts.noShell {
		return fmt.Errorf("AGENT_OBSERVER_SHELL and AGENT_OBSERVER_NO_SHELL cannot both enable conflicting shell policy")
	}
	if opts.maxFileSize <= 0 {
		return fmt.Errorf("--max-file-size must be greater than zero")
	}
	if opts.refreshInterval < minRefreshInterval || opts.refreshInterval > maxRefreshInterval {
		return fmt.Errorf("--refresh-interval must be between %s and %s", minRefreshInterval, maxRefreshInterval)
	}
	opts.focus = strings.ToLower(strings.TrimSpace(opts.focus))
	switch opts.focus {
	case "all", "active", "blocked", "warnings":
	default:
		return fmt.Errorf("invalid --focus %q", opts.focus)
	}
	opts.logLevel = strings.ToLower(strings.TrimSpace(opts.logLevel))
	switch opts.logLevel {
	case "", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid --log-level %q", opts.logLevel)
	}
	opts.telemetry = strings.ToLower(strings.TrimSpace(opts.telemetry))
	switch opts.telemetry {
	case "", "off", "on":
	default:
		return fmt.Errorf("invalid --telemetry %q", opts.telemetry)
	}
	if opts.telemetry == "" {
		opts.telemetry = "off"
	}
	if opts.telemetryURL != "" {
		parsed, err := url.Parse(opts.telemetryURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("invalid --telemetry-endpoint %q", opts.telemetryURL)
		}
		switch parsed.Scheme {
		case "http", "https":
		default:
			return fmt.Errorf("--telemetry-endpoint must use http or https")
		}
	}
	return nil
}

func markSeenFlags(fs *flag.FlagSet, opts *options) {
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "shell":
			opts.shellFlagSeen = true
		case "no-shell":
			opts.noShellFlagSeen = true
		}
	})
}

func normalizeShellOptions(opts *options) {
	if opts.shellFlagSeen && opts.shell {
		opts.noShell = false
	}
	if opts.noShellFlagSeen && opts.noShell {
		opts.shell = false
	}
}

func shellEnabled(opts options) bool {
	if opts.noShell {
		return false
	}
	return opts.shell
}
