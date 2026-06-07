package agentauth

// Capability flags grant tasks additional capabilities beyond their own task
// data. They are workspace-level capabilities, not authscope grammar scopes.
const (
	// CapabilityGitHubToken allows issuing GitHub App installation tokens via
	// the CreateGitHubToken RPC.
	CapabilityGitHubToken = "github_token"
	// CapabilityChildTasks allows creating, listing, updating, and reading logs
	// of child tasks.
	CapabilityChildTasks = "child_tasks"
)

// ValidCapability reports whether capability is a recognized capability flag.
func ValidCapability(capability string) bool {
	switch capability {
	case CapabilityGitHubToken, CapabilityChildTasks:
		return true
	default:
		return false
	}
}
