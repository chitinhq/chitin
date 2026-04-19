package normalize

// ActionType is one of 6 canonical action categories.
// v2 simplification: 6 types instead of v1's 43.
type ActionType string

const (
	Read      ActionType = "read"
	Write     ActionType = "write"
	Exec      ActionType = "exec"
	Git       ActionType = "git"
	Net       ActionType = "net"
	Dangerous ActionType = "dangerous"
)

// Action is the normalized representation of a tool call.
type Action struct {
	Type    ActionType
	Tool    string            // original tool name (e.g., "Bash", "Write", "Read")
	Path    string            // file path for read/write operations
	Command string            // shell command for exec, git subcommand for git
	Content string            // file content for write operations
	Input   map[string]any    // raw tool input (preserved for invariant checks)
}
