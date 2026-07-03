package source

// LaunchRequest describes a new agent run started from the dashboard.
type LaunchRequest struct {
	Prompt string
	CWD    string
}

// Actor is implemented by adapters whose provider can be driven through a
// local CLI. Argv results are executed directly (never via a shell) and are
// always displayed to the user before and while running.
type Actor interface {
	// CanAct reports whether the provider CLI is resolvable right now.
	CanAct() bool
	// LaunchArgv returns the command for a fresh interactive run.
	LaunchArgv(req LaunchRequest) ([]string, error)
	// ResumeArgv returns the command that resumes an observed session.
	ResumeArgv(sessionID, cwd string) ([]string, error)
}
