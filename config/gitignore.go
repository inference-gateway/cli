package config

import (
	"os"
	"path/filepath"
)

// InferGitignoreContent is the .gitignore seeded into a project-local ./.infer/
// so runtime artifacts (logs, conversations, downloaded binaries, backups) are
// never committed. Shared by `infer init` and the runtime auto-provisioner so
// both stay in lockstep.
const InferGitignoreContent = `# inference-gateway CLI runtime artifacts — do not commit
logs/*.log
history
chat_export_*
conversations.db*
conversations
session_groups.json
bin/
backups/
tmp/
plans/
`

// EnsureProjectGitignore writes ./.infer/.gitignore if it is absent, creating
// ./.infer/ if needed. It is idempotent: an existing file is left untouched so
// `infer init --project` output and user edits are preserved.
//
// It targets the bare project-relative ConfigDirName (not GetConfigDir()) on
// purpose: the runtime artifacts it guards — logs, conversations, the gateway
// binary, backups — always land in the project-local ./.infer/ regardless of
// where config itself resolves.
func EnsureProjectGitignore() error {
	path := filepath.Join(ConfigDirName, GitignoreFileName)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(ConfigDirName, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(InferGitignoreContent), 0o644)
}
