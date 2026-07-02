// Package manifest turns declarative provider descriptions into adapters,
// so new agent tools can be observed (and later launched) without recompiling.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Manifest describes one external provider. Files live in the plugins
// directory as <name>.toml or <name>.json and are trusted local config.
type Manifest struct {
	Name     string         `toml:"name" json:"name"`
	Watch    WatchConfig    `toml:"watch" json:"watch"`
	Session  SessionConfig  `toml:"session" json:"session"`
	Commands CommandsConfig `toml:"commands" json:"commands"`
}

type WatchConfig struct {
	// Globs are filepath.Glob patterns; a leading ~ expands to the home dir.
	Globs []string `toml:"globs" json:"globs"`
}

type SessionConfig struct {
	// ID is "filename_stem", "dir_name", or "field:<dotted.path>".
	ID string `toml:"id" json:"id"`
	// CWD and Text are "field:<dotted.path>" extractions from JSONL lines.
	CWD    string       `toml:"cwd" json:"cwd"`
	Text   string       `toml:"text" json:"text"`
	Status StatusConfig `toml:"status" json:"status"`
}

type StatusConfig struct {
	Mode       string   `toml:"mode" json:"mode"` // only "recency" is supported
	BusyWithin duration `toml:"busy_within" json:"busy_within"`
	IdleWithin duration `toml:"idle_within" json:"idle_within"`
}

type CommandsConfig struct {
	// Argv arrays with {prompt}, {session_id}, {cwd} placeholders replaced
	// per element; never joined through a shell.
	Launch []string `toml:"launch" json:"launch"`
	Resume []string `toml:"resume" json:"resume"`
}

// duration accepts Go duration strings in both TOML and JSON.
type duration struct {
	time.Duration
}

func (d *duration) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return nil
	}
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func (d *duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	return d.UnmarshalText([]byte(value))
}

// Load parses every manifest in dir. Invalid manifests are reported as
// errors keyed by path; valid ones still load.
func Load(dir string) ([]Manifest, map[string]error) {
	var manifests []Manifest
	errs := map[string]error{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".toml" && ext != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		m, err := parseFile(path)
		if err != nil {
			errs[path] = err
			continue
		}
		manifests = append(manifests, m)
	}
	return manifests, errs
}

func parseFile(path string) (Manifest, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if filepath.Ext(path) == ".json" {
		err = json.Unmarshal(payload, &m)
	} else {
		err = toml.Unmarshal(payload, &m)
	}
	if err != nil {
		return Manifest{}, err
	}
	if err := m.validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (m Manifest) validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("manifest requires a name")
	}
	if len(m.Watch.Globs) == 0 {
		return fmt.Errorf("manifest %q requires watch.globs", m.Name)
	}
	switch m.Session.ID {
	case "", "filename_stem", "dir_name":
	default:
		if !strings.HasPrefix(m.Session.ID, "field:") {
			return fmt.Errorf("manifest %q: session.id must be filename_stem, dir_name, or field:<path>", m.Name)
		}
	}
	for _, value := range []string{m.Session.CWD, m.Session.Text} {
		if value != "" && !strings.HasPrefix(value, "field:") {
			return fmt.Errorf("manifest %q: session extractors must use field:<path>", m.Name)
		}
	}
	if mode := m.Session.Status.Mode; mode != "" && mode != "recency" {
		return fmt.Errorf("manifest %q: unsupported status mode %q", m.Name, mode)
	}
	for _, argv := range [][]string{m.Commands.Launch, m.Commands.Resume} {
		if len(argv) == 1 && strings.ContainsAny(argv[0], " |&;") {
			return fmt.Errorf("manifest %q: commands must be argv arrays, not shell strings", m.Name)
		}
	}
	return nil
}

// extractField walks a dotted path through a decoded JSON object. It
// descends into the last element of arrays, which matches how transcript
// content blocks are usually shaped.
func extractField(value any, path string) string {
	for _, key := range strings.Split(path, ".") {
		for {
			if list, ok := value.([]any); ok {
				if len(list) == 0 {
					return ""
				}
				value = list[len(list)-1]
				continue
			}
			break
		}
		object, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = object[key]
	}
	for {
		if list, ok := value.([]any); ok {
			if len(list) == 0 {
				return ""
			}
			value = list[len(list)-1]
			continue
		}
		break
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
