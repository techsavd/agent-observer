package cursor

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/techsavd/agent-observer/core/source"
)

func (a *Adapter) CanAct() bool {
	_, err := exec.LookPath("cursor-agent")
	return err == nil
}

func (a *Adapter) LaunchArgv(req source.LaunchRequest) ([]string, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return []string{"cursor-agent"}, nil
	}
	return []string{"cursor-agent", req.Prompt}, nil
}

func (a *Adapter) ResumeArgv(sessionID, _ string) ([]string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("cursor resume requires a session id")
	}
	return []string{"cursor-agent", "--resume", sessionID}, nil
}
