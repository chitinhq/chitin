package normalize

import (
	"testing"
)

func TestNormalize_ReadActions(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		input    map[string]any
		expected ActionType
	}{
		{
			name: "Read tool with file_path",
			tool: "Read",
			input: map[string]any{
				"file_path": "/path/to/file.txt",
			},
			expected: Read,
		},
		{
			name: "Read tool with path",
			tool: "Read",
			input: map[string]any{
				"path": "/path/to/file.txt",
			},
			expected: Read,
		},
		{
			name: "glob tool",
			tool: "glob",
			input: map[string]any{
				"pattern": "*.go",
			},
			expected: Read,
		},
		{
			name: "grep tool",
			tool: "grep",
			input: map[string]any{
				"pattern": "test",
			},
			expected: Read,
		},
		{
			name: "ls tool",
			tool: "ls",
			input: map[string]any{
				"directory": ".",
			},
			expected: Read,
		},
		{
			name: "notebookread tool",
			tool: "notebookread",
			input: map[string]any{
				"path": "notebook.ipynb",
			},
			expected: Read,
		},
		{
			name: "case insensitive read",
			tool: "READ",
			input: map[string]any{
				"path": "/path/to/file.txt",
			},
			expected: Read,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := Normalize(tt.tool, tt.input)
			if action.Type != tt.expected {
				t.Errorf("Normalize(%q, ...) = %v, want %v", tt.tool, action.Type, tt.expected)
			}
			if action.Tool != tt.tool {
				t.Errorf("Normalize(%q, ...).Tool = %v, want %v", tt.tool, action.Tool, tt.tool)
			}
		})
	}
}

func TestNormalize_WriteActions(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		input    map[string]any
		expected ActionType
	}{
		{
			name: "Write tool with content",
			tool: "Write",
			input: map[string]any{
				"path":    "/path/to/file.txt",
				"content": "file content",
			},
			expected: Write,
		},
		{
			name: "edit tool",
			tool: "edit",
			input: map[string]any{
				"path":     "/path/to/file.txt",
				"old_text": "old",
				"new_text": "new",
			},
			expected: Write,
		},
		{
			name: "notebookedit tool",
			tool: "notebookedit",
			input: map[string]any{
				"path": "notebook.ipynb",
				"cell": "some cell content",
			},
			expected: Write,
		},
		{
			name: "case insensitive write",
			tool: "WRITE",
			input: map[string]any{
				"path":    "/path/to/file.txt",
				"content": "content",
			},
			expected: Write,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := Normalize(tt.tool, tt.input)
			if action.Type != tt.expected {
				t.Errorf("Normalize(%q, ...) = %v, want %v", tt.tool, action.Type, tt.expected)
			}
		})
	}
}

func TestNormalize_ExecActions(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		input    map[string]any
		expected ActionType
	}{
		{
			name: "agent tool",
			tool: "agent",
			input: map[string]any{
				"task": "do something",
			},
			expected: Exec,
		},
		{
			name: "task tool",
			tool: "task",
			input: map[string]any{
				"description": "task description",
			},
			expected: Exec,
		},
		{
			name: "taskcreate tool",
			tool: "taskcreate",
			input: map[string]any{
				"task": "new task",
			},
			expected: Exec,
		},
		{
			name: "taskupdate tool",
			tool: "taskupdate",
			input: map[string]any{
				"task_id": "123",
				"status":  "completed",
			},
			expected: Exec,
		},
		{
			name: "tasklist tool",
			tool: "tasklist",
			input: map[string]any{},
			expected: Exec,
		},
		{
			name: "taskget tool",
			tool: "taskget",
			input: map[string]any{
				"task_id": "123",
			},
			expected: Exec,
		},
		{
			name: "unknown tool defaults to exec",
			tool: "unknown_tool",
			input: map[string]any{
				"some": "input",
			},
			expected: Exec,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := Normalize(tt.tool, tt.input)
			if action.Type != tt.expected {
				t.Errorf("Normalize(%q, ...) = %v, want %v", tt.tool, action.Type, tt.expected)
			}
		})
	}
}

func TestNormalize_NetActions(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		input    map[string]any
		expected ActionType
	}{
		{
			name: "webfetch tool",
			tool: "webfetch",
			input: map[string]any{
				"url": "https://example.com",
			},
			expected: Net,
		},
		{
			name: "websearch tool",
			tool: "websearch",
			input: map[string]any{
				"query": "test query",
			},
			expected: Net,
		},
		{
			name: "mcp__ prefixed tool",
			tool: "mcp__something",
			input: map[string]any{
				"param": "value",
			},
			expected: Net,
		},
		{
			name: "case insensitive mcp prefix",
			tool: "MCP__TOOL",
			input: map[string]any{
				"param": "value",
			},
			expected: Net,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := Normalize(tt.tool, tt.input)
			if action.Type != tt.expected {
				t.Errorf("Normalize(%q, ...) = %v, want %v", tt.tool, action.Type, tt.expected)
			}
		})
	}
}

func TestNormalize_BashCommands(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected ActionType
	}{
		// Git commands
		{
			name:     "git status",
			command:  "git status",
			expected: Git,
		},
		{
			name:     "git log",
			command:  "git log --oneline",
			expected: Git,
		},
		{
			name:     "git clone",
			command:  "git clone https://github.com/example/repo.git",
			expected: Git,
		},
		{
			name:     "git fetch",
			command:  "git fetch origin",
			expected: Git,
		},
		{
			name:     "git pull",
			command:  "git pull origin main",
			expected: Git,
		},
		{
			name:     "git add",
			command:  "git add file.txt",
			expected: Git,
		},
		{
			name:     "git commit",
			command:  "git commit -m 'message'",
			expected: Git,
		},
		{
			name:     "git merge",
			command:  "git merge feature",
			expected: Git,
		},
		{
			name:     "git branch",
			command:  "git branch new-feature",
			expected: Git,
		},

		// Network commands
		{
			name:     "curl",
			command:  "curl https://example.com",
			expected: Net,
		},
		{
			name:     "wget",
			command:  "wget https://example.com/file.txt",
			expected: Net,
		},
		{
			name:     "gh",
			command:  "gh pr create",
			expected: Net,
		},
		{
			name:     "ssh",
			command:  "ssh user@host",
			expected: Net,
		},
		{
			name:     "scp",
			command:  "scp file.txt user@host:/path",
			expected: Net,
		},
		{
			name:     "rsync",
			command:  "rsync -av source/ dest/",
			expected: Net,
		},

		// Dangerous commands
		{
			name:     "rm",
			command:  "rm file.txt",
			expected: Dangerous,
		},
		{
			name:     "rm recursive",
			command:  "rm -rf directory",
			expected: Dangerous,
		},
		{
			name:     "dd",
			command:  "dd if=/dev/zero of=/dev/sda",
			expected: Dangerous,
		},
		{
			name:     "git push",
			command:  "git push origin main",
			expected: Dangerous,
		},
		{
			name:     "git push force",
			command:  "git push --force origin main",
			expected: Dangerous,
		},
		{
			name:     "git reset",
			command:  "git reset --hard HEAD~1",
			expected: Dangerous,
		},
		{
			name:     "git clean",
			command:  "git clean -fd",
			expected: Dangerous,
		},
		{
			name:     "git checkout",
			command:  "git checkout .",
			expected: Dangerous,
		},
		{
			name:     "git restore",
			command:  "git restore file.txt",
			expected: Dangerous,
		},
		{
			name:     "chmod 777",
			command:  "chmod 777 file.txt",
			expected: Dangerous,
		},
		{
			name:     "chmod u+s",
			command:  "chmod u+s file.txt",
			expected: Dangerous,
		},
		{
			name:     "chmod g+s",
			command:  "chmod g+s file.txt",
			expected: Dangerous,
		},
		{
			name:     "chmod with mode flag 777",
			command:  "chmod --mode=777 file.txt",
			expected: Dangerous,
		},

		// Regular exec commands
		{
			name:     "ls command",
			command:  "ls -la",
			expected: Exec,
		},
		{
			name:     "echo command",
			command:  "echo 'hello'",
			expected: Exec,
		},
		{
			name:     "cat command",
			command:  "cat file.txt",
			expected: Exec,
		},
		{
			name:     "mkdir command",
			command:  "mkdir newdir",
			expected: Exec,
		},
		{
			name:     "cp command",
			command:  "cp source.txt dest.txt",
			expected: Exec,
		},
		{
			name:     "mv command",
			command:  "mv old.txt new.txt",
			expected: Exec,
		},
		{
			name:     "chmod safe",
			command:  "chmod 644 file.txt",
			expected: Exec,
		},
		{
			name:     "chmod +x",
			command:  "chmod +x script.sh",
			expected: Exec,
		},
		{
			name:     "find command",
			command:  "find . -name '*.go'",
			expected: Exec,
		},
		{
			name:     "grep command",
			command:  "grep pattern file.txt",
			expected: Exec,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := Normalize("bash", map[string]any{
				"command": tt.command,
			})
			if action.Type != tt.expected {
				t.Errorf("Normalize('bash', {command: %q}) = %v, want %v", tt.command, action.Type, tt.expected)
			}
			if action.Command != tt.command {
				t.Errorf("Normalize('bash', {command: %q}).Command = %v, want %v", tt.command, action.Command, tt.command)
			}
		})
	}
}

func TestNormalize_FieldExtraction(t *testing.T) {
	tests := []struct {
		name          string
		tool          string
		input         map[string]any
		expectedPath  string
		expectedCmd   string
		expectedContent string
	}{
		{
			name: "extract file_path",
			tool: "Read",
			input: map[string]any{
				"file_path": "/path/to/file.txt",
			},
			expectedPath: "/path/to/file.txt",
		},
		{
			name: "extract path (fallback)",
			tool: "Read",
			input: map[string]any{
				"path": "/another/path.txt",
			},
			expectedPath: "/another/path.txt",
		},
		{
			name: "prefer file_path over path",
			tool: "Read",
			input: map[string]any{
				"file_path": "/file_path.txt",
				"path":      "/path.txt",
			},
			expectedPath: "/file_path.txt",
		},
		{
			name: "extract command",
			tool: "bash",
			input: map[string]any{
				"command": "ls -la",
			},
			expectedCmd: "ls -la",
		},
		{
			name: "extract content",
			tool: "Write",
			input: map[string]any{
				"path":    "/file.txt",
				"content": "file content here",
			},
			expectedPath:    "/file.txt",
			expectedContent: "file content here",
		},
		{
			name: "extract all fields",
			tool: "Write",
			input: map[string]any{
				"file_path": "/test.txt",
				"content":   "test content",
				"command":   "should not be extracted for write",
			},
			expectedPath:    "/test.txt",
			expectedContent: "test content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := Normalize(tt.tool, tt.input)
			
			if tt.expectedPath != "" && action.Path != tt.expectedPath {
				t.Errorf("Path = %q, want %q", action.Path, tt.expectedPath)
			}
			if tt.expectedCmd != "" && action.Command != tt.expectedCmd {
				t.Errorf("Command = %q, want %q", action.Command, tt.expectedCmd)
			}
			if tt.expectedContent != "" && action.Content != tt.expectedContent {
				t.Errorf("Content = %q, want %q", action.Content, tt.expectedContent)
			}
			
			// Verify input is preserved
			if action.Input == nil {
				t.Error("Input should not be nil")
			}
		})
	}
}

func TestNormalize_InputPreservation(t *testing.T) {
	input := map[string]any{
		"file_path": "/test.txt",
		"content":   "test",
		"extra":     "field",
		"nested": map[string]any{
			"key": "value",
		},
	}
	
	action := Normalize("Write", input)
	
	// Check that input is preserved (same reference)
	if action.Input == nil {
		t.Error("Input should not be nil")
	}
	
	// Verify we can access the extra field
	if extra, ok := action.Input["extra"].(string); !ok || extra != "field" {
		t.Error("Extra field not preserved correctly")
	}
	
	// Verify nested structure
	if nested, ok := action.Input["nested"].(map[string]any); !ok || nested["key"] != "value" {
		t.Error("Nested field not preserved correctly")
	}
}