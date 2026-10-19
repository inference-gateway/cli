package setup

import (
	"fmt"
	"strings"
)

// InstallSkill is the catalog skill carrying the infer-action workflow
// authoring guide and its bundled examples. Mentioning it as "/<name>" in the
// agent prompt activates it (downloaded from the skills catalog on demand).
const InstallSkill = "opentask"

// taskAllowRule keeps the Taskfile runner usable in every generated workflow,
// since the CLI's bash baseline no longer auto-approves task.
const taskAllowRule = "The infer-action bash-allow-append must start with `task( .*)?` - add it when missing, since the CLI's baseline does not allow task and the agent needs it to run the repo's tasks."

// InstallBranch is the fixed head branch for agent-driven workflow installs.
// Keeping it stable means a re-install pushes onto the same branch and updates
// the already-open PR instead of opening a duplicate.
const InstallBranch = "infer/install-github-action"

// InstallChatPrompt builds the install task submitted as a regular chat
// message - the single canonical prompt for installing the OpenTask workflow,
// used by the /install-opentask shortcut and (via that shortcut) the browser
// extension's Install tab. repo may be empty (the agent detects the current
// checkout's repo); context carries extra user/workflow configuration.
func InstallChatPrompt(repo, context string) string {
	target := "the repository of the current checkout (detect it with `gh repo view`)"
	if strings.TrimSpace(repo) != "" {
		target = repo
	}
	lines := []string{
		"/" + InstallSkill,
		fmt.Sprintf("Install or update the OpenTask GitHub workflow in %s. Read the /%s skill's guide and its bundled example workflows FIRST - they are the canonical infer-action usage patterns. Do not fetch infer-action docs or examples from the network.", target, InstallSkill),
		"1. If the current checkout already is the target repo, add a git worktree under /tmp for the install branch so the checked-out branch stays untouched; otherwise clone the target repo (shallow) into a temp dir under /tmp.",
		fmt.Sprintf("2. Check out branch %s, on top of origin's if it already exists (a re-install must update the open PR, not open a duplicate).", InstallBranch),
		"3. Create or update .github/workflows/tasks.yml for infer-action following the skill's canonical workflow and the repo's existing CI conventions, preserving any repo-specific customizations. " + taskAllowRule,
		"4. Show me a short summary of the changes, then commit, push the branch, and open (or update) the pull request. Wait for my approval on the push and PR creation.",
	}
	if strings.TrimSpace(context) != "" {
		lines = append(lines, "", "Workflow configuration:", strings.TrimSpace(context))
	}
	return strings.Join(lines, "\n")
}
