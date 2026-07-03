package codex

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/techsavd/agent-observer/core/source"
)

func (a *Adapter) CanAct() bool {
	_, err := exec.LookPath("codex")
	return err == nil
}

func (a *Adapter) LaunchArgv(req source.LaunchRequest) ([]string, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return []string{"codex"}, nil
	}
	return []string{"codex", req.Prompt}, nil
}

func (a *Adapter) ResumeArgv(sessionID, _ string) ([]string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("codex resume requires a session id")
	}
	return []string{"codex", "resume", sessionID}, nil
}
