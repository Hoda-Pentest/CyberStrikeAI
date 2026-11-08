package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func sanitizeWorkspacePathSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
	}
	s = strings.ReplaceAll(s, string(filepath.Separator), "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	s = strings.ReplaceAll(s, "..", "__")
	if len(s) > 180 {
		s = s[:180]
	}
	return s
}

// WorkspaceRootDir returns the relative workspace root for downloads and local analysis.
// Project-bound sessions share projects/<id>/; otherwise conversations/<id>/.
func WorkspaceRootDir(configuredBase, projectID, conversationID string) string {
	base := strings.TrimSpace(configuredBase)
	if base == "" {
		base = filepath.Join("tmp", "workspace")
	}
	if pid := strings.TrimSpace(projectID); pid != "" {
		return filepath.Join(base, "projects", sanitizeWorkspacePathSegment(pid))
	}
	conv := strings.TrimSpace(conversationID)
	if conv == "" {
		conv = "default"
	}
	return filepath.Join(base, "conversations", sanitizeWorkspacePathSegment(conv))
}

// EnsureWorkspace creates the workspace directory and returns its absolute path.
func EnsureWorkspace(root string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return "", fmt.Errorf("workspace abs: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("workspace mkdir: %w", err)
	}
	return abs, nil
}

// BuildWorkspaceBlock instructs the agent to use the session workspace instead of /tmp.
func BuildWorkspaceBlock(absPath string) string {
	absPath = strings.TrimSpace(absPath)
	if absPath == "" {
		return ""
	}
	return fmt.Sprintf(`## Session working directory (downloads and local analysis)

**You must use the following directory** to store files downloaded with curl/wget, temporary HTML/JS, and as the search scope for read_file/glob/grep:
`+"`%s`"+`

- When a project is bound, this directory is shared by sessions of the same project; after a restart or reopening the conversation, still search existing results first. Working scripts and dependencies are saved under `+"`artifacts/`"+`, and registered with `+"`upsert_project_fact(category=note, fact_key=artifact/<name>)`"+` recording the relative path, invocation, and verification evidence.
- **Do not** use the system `+"`/tmp`"+` or other global temp directories (files would leak between projects/sessions).
- Download example: `+"`curl -o '%s/page.html' 'https://target/'`"+`; for exec you can set `+"`workdir`"+` to this directory.
- Before reading downloaded artifacts or temporary analysis files, search with glob/grep/read_file **confined to this directory**; do not blindly search in `+"`/tmp`"+`.
- When the user asks about the "current directory", the "project root", or the application's own files, interpret it as the service process's current working directory first; do not mistake an empty session working directory for the project root.`, absPath, absPath)
}
