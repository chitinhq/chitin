package gov

import (
	"testing"
)

func TestNormalizeExecuteCode_WithShellOut(t *testing.T) {
	// Code containing a shell-out pattern — rm -rf gets re-tagged
	// as the more specific file.recursive_delete, not generic shell.exec
	args := map[string]any{"code": "os.system('rm -rf /tmp/x')"}
	action := normalizeExecuteCode(args)
	// The command is destructive, so it gets a specific action type
	if action.Type == ActFileWrite {
		t.Errorf("shell-out code should not stay as ActFileWrite")
	}
}

func TestNormalizeExecuteCode_PureCode(t *testing.T) {
	// Pure Python without shell-out should be file.write
	args := map[string]any{"code": "print('hello world')"}
	action := normalizeExecuteCode(args)
	if action.Type != ActFileWrite {
		t.Errorf("expected ActFileWrite for pure code, got %v", action.Type)
	}
	if action.Target != "execute_code" {
		t.Errorf("expected target 'execute_code', got %q", action.Target)
	}
}

func TestNormalizeWriteFile_WithPath(t *testing.T) {
	args := map[string]any{"path": "/tmp/test.py"}
	action := normalizeWriteFile(args)
	if action.Type != ActFileWrite {
		t.Errorf("expected ActFileWrite, got %v", action.Type)
	}
	if action.Target != "/tmp/test.py" {
		t.Errorf("expected target '/tmp/test.py', got %q", action.Target)
	}
}

func TestNormalizeWriteFile_WithFilePath(t *testing.T) {
	// Some drivers use file_path instead of path
	args := map[string]any{"file_path": "/home/user/code.py"}
	action := normalizeWriteFile(args)
	if action.Target != "/home/user/code.py" {
		t.Errorf("expected target '/home/user/code.py', got %q", action.Target)
	}
}

func TestNormalizeWriteFile_NoPath(t *testing.T) {
	args := map[string]any{}
	action := normalizeWriteFile(args)
	if action.Target != "" {
		t.Errorf("expected empty target for missing path, got %q", action.Target)
	}
}