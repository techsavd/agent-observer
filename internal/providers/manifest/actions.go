package manifest

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/techsavd/agent-observer/core/source"
)

func (a *Adapter) CanAct() bool {
	argv := a.manifest.Commands.Launch
	if len(argv) == 0 {
		argv = a.manifest.Commands.Resume
	}
	if len(argv) == 0 {
		return false
	}
	_, err := exec.LookPath(argv[0])
	return err == nil
}

func (a *Adapter) LaunchArgv(req source.LaunchRequest) ([]string, error) {
	if len(a.manifest.Commands.Launch) == 0 {
		return nil, fmt.Errorf("provider %s has no launch command", a.manifest.Name)
	}
	return substituteArgv(a.manifest.Commands.Launch, map[string]string{
		"{prompt}": req.Prompt,
		"{cwd}":    req.CWD,
	}), nil
}

func (a *Adapter) ResumeArgv(sessionID, cwd string) ([]string, error) {
	if len(a.manifest.Commands.Resume) == 0 {
		return nil, fmt.Errorf("provider %s has no resume command", a.manifest.Name)
	}
	return substituteArgv(a.manifest.Commands.Resume, map[string]string{
		"{session_id}": sessionID,
		"{cwd}":        cwd,
	}), nil
}

// substituteArgv replaces placeholders inside individual argv elements.
// Values are never re-split or passed through a shell, so a hostile value
// stays one argument.
func substituteArgv(argv []string, values map[string]string) []string {
	out := make([]string, len(argv))
	for i, arg := range argv {
		for placeholder, value := range values {
			arg = strings.ReplaceAll(arg, placeholder, value)
		}
		out[i] = arg
	}
	return out
}
