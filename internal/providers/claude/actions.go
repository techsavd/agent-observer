package claude

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/techsavd/agent-observer/core/source"
)

func (a *Adapter) CanAct() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

func (a *Adapter) LaunchArgv(req source.LaunchRequest) ([]string, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return []string{"claude"}, nil
	}
	return []string{"claude", req.Prompt}, nil
}

func (a *Adapter) ResumeArgv(sessionID, _ string) ([]string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("claude resume requires a session id")
	}
	return []string{"claude", "--resume", sessionID}, nil
}
