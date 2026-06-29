package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	utils "github.com/inference-gateway/cli/config/utils"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Inference Gateway CLI configuration",
	Long: `Initialize Inference Gateway CLI configuration in the user home directory (~/.infer/).

By default, this seeds the full baseline configuration to ~/.infer/ so it is shared
across all of your projects.  Pass --project to create a project-level override layer
in ./.infer/ instead — only project-overridable files are seeded there as a sparse
scaffold; personal, machine-, or secret-scoped files always live in ~/.infer/.

To generate an AGENTS.md file, use the /init shortcut in interactive chat mode,
which allows you to see the agent's analysis in real-time.

This is the recommended command to start working with Inference Gateway CLI.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return initializeProject(cmd)
	},
}

func init() {
	initCmd.Flags().Bool("overwrite", false, "Overwrite existing files if they already exist")
	initCmd.Flags().Bool("project", false, "Initialize a project override layer in ./.infer/ (sparse scaffold only)")
	initCmd.Flags().Bool("skip-migrations", false, "Skip running database migrations")
	rootCmd.AddCommand(initCmd)
}

func initializeProject(cmd *cobra.Command) error { //nolint:funlen,gocyclo,cyclop,gocognit
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	project, _ := cmd.Flags().GetBool("project")
	skipMigrations, _ := cmd.Flags().GetBool("skip-migrations")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}
	homeCfgDir := filepath.Join(homeDir, config.ConfigDirName)

	// Userspace-only file paths — always seeded to ~/.infer/, regardless of scope.
	homeKeybindingsPath := filepath.Join(homeCfgDir, config.KeybindingsFileName)
	homeremindersPath := filepath.Join(homeCfgDir, config.RemindersFileName)
	homeChannelsPath := filepath.Join(homeCfgDir, config.ChannelsFileName)
	homeHeartbeatPath := filepath.Join(homeCfgDir, config.HeartbeatFileName)
	homeComputerUsePath := filepath.Join(homeCfgDir, config.ComputerUseFileName)
	homeMemoryConfigPath := filepath.Join(homeCfgDir, config.MemoryConfigFileName)

	// Project-overridable file paths — these go to ./.infer/ in --project mode
	// or to ~/.infer/ in default (home) mode.
	var configPath, gitignorePath, scmShortcutsPath, gitShortcutsPath,
		mcpShortcutsPath, shellsShortcutsPath, exportShortcutsPath,
		envShortcutsPath, a2aShortcutsPath, skillsShortcutsPath, mcpPath, promptsPath,
		hooksPath, agentsPath, skillsDirPath string

	// Userspace-only paths — always home. These are assigned once and used in
	// both modes so the creation logic below is shared.
	keybindingsPath := homeKeybindingsPath
	remindersPath := homeremindersPath
	channelsPath := homeChannelsPath
	heartbeatPath := homeHeartbeatPath
	computerUsePath := homeComputerUsePath
	memoryConfigPath := homeMemoryConfigPath

	if project {
		// --project: seed only project-overridable files to ./.infer/ as a sparse
		// scaffold. Userspace-only files always go to ~/.infer/ (assigned above).
		configPath = config.DefaultConfigPath
		gitignorePath = filepath.Join(config.ConfigDirName, config.GitignoreFileName)
		scmShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "scm.yaml")
		gitShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "git.yaml")
		mcpShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "mcp.yaml")
		shellsShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "shells.yaml")
		exportShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "export.yaml")
		envShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "env.yaml")
		a2aShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "a2a.yaml")
		skillsShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "skills.yaml")
		mcpPath = filepath.Join(config.ConfigDirName, config.MCPFileName)
		promptsPath = filepath.Join(config.ConfigDirName, config.PromptsFileName)
		hooksPath = filepath.Join(config.ConfigDirName, config.HooksFileName)
		agentsPath = filepath.Join(config.ConfigDirName, config.AgentsFileName)
		skillsDirPath = filepath.Join(config.ConfigDirName, "skills")
	} else {
		// Default (home): seed the full baseline to ~/.infer/.
		configPath = filepath.Join(homeCfgDir, config.ConfigFileName)
		gitignorePath = filepath.Join(homeCfgDir, config.GitignoreFileName)
		scmShortcutsPath = filepath.Join(homeCfgDir, "shortcuts", "scm.yaml")
		gitShortcutsPath = filepath.Join(homeCfgDir, "shortcuts", "git.yaml")
		mcpShortcutsPath = filepath.Join(homeCfgDir, "shortcuts", "mcp.yaml")
		shellsShortcutsPath = filepath.Join(homeCfgDir, "shortcuts", "shells.yaml")
		exportShortcutsPath = filepath.Join(homeCfgDir, "shortcuts", "export.yaml")
		envShortcutsPath = filepath.Join(homeCfgDir, "shortcuts", "env.yaml")
		a2aShortcutsPath = filepath.Join(homeCfgDir, "shortcuts", "a2a.yaml")
		skillsShortcutsPath = filepath.Join(homeCfgDir, "shortcuts", "skills.yaml")
		mcpPath = filepath.Join(homeCfgDir, config.MCPFileName)
		promptsPath = filepath.Join(homeCfgDir, config.PromptsFileName)
		hooksPath = filepath.Join(homeCfgDir, config.HooksFileName)
		agentsPath = filepath.Join(homeCfgDir, config.AgentsFileName)
		skillsDirPath = filepath.Join(homeCfgDir, "skills")
	}

	// Validate: only fail if the *freshly seeded* files already exist.
	// In --project mode, userspace-only files may already exist in ~/.infer/
	// from a prior home init — those are seeded only-if-absent below, so we
	// exclude them from the existence check.
	if !overwrite {
		// Project-overridable files are always freshly seeded, so none of them
		// may pre-exist. The userspace-only files (keybindings, reminders,
		// channels, heartbeat, computer_use) are seeded only-if-absent, so they
		// are only checked in home mode - in --project mode they may legitimately
		// already exist from an earlier home init.
		pathsToCheck := []string{
			configPath, gitignorePath, scmShortcutsPath, gitShortcutsPath,
			mcpShortcutsPath, shellsShortcutsPath, exportShortcutsPath,
			envShortcutsPath, a2aShortcutsPath, skillsShortcutsPath,
			mcpPath, promptsPath, hooksPath, agentsPath,
		}
		if !project {
			pathsToCheck = append(pathsToCheck,
				keybindingsPath, remindersPath, channelsPath, heartbeatPath, computerUsePath)
		}
		if err := validateFilesNotExist(pathsToCheck...); err != nil {
			return err
		}
	}

	// --- Create project-overridable files ---
	if project {
		if err := createSparseConfigScaffold(configPath); err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}
	} else {
		if err := utils.SaveYAML(configPath, "config", config.DefaultConfig()); err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}
	}

	if err := os.WriteFile(gitignorePath, []byte(config.InferGitignoreContent), 0o644); err != nil {
		return fmt.Errorf("failed to create .gitignore file: %w", err)
	}

	if err := createSCMShortcutsFile(scmShortcutsPath); err != nil {
		return fmt.Errorf("failed to create SCM shortcuts file: %w", err)
	}

	if err := createGitShortcutsFile(gitShortcutsPath); err != nil {
		return fmt.Errorf("failed to create Git shortcuts file: %w", err)
	}

	if err := createMCPShortcutsFile(mcpShortcutsPath); err != nil {
		return fmt.Errorf("failed to create MCP shortcuts file: %w", err)
	}

	if err := createShellsShortcutsFile(shellsShortcutsPath); err != nil {
		return fmt.Errorf("failed to create Shells shortcuts file: %w", err)
	}

	if err := createExportShortcutsFile(exportShortcutsPath); err != nil {
		return fmt.Errorf("failed to create Export shortcuts file: %w", err)
	}

	if err := createEnvShortcutsFile(envShortcutsPath); err != nil {
		return fmt.Errorf("failed to create Env shortcuts file: %w", err)
	}

	if err := createA2AShortcutsFile(a2aShortcutsPath); err != nil {
		return fmt.Errorf("failed to create A2A shortcuts file: %w", err)
	}

	if err := createSkillsShortcutsFile(skillsShortcutsPath); err != nil {
		return fmt.Errorf("failed to create Skills shortcuts file: %w", err)
	}

	if err := createMCPConfigFile(mcpPath); err != nil {
		return fmt.Errorf("failed to create MCP config file: %w", err)
	}

	if err := createPromptsConfigFile(promptsPath); err != nil {
		return fmt.Errorf("failed to create prompts config file: %w", err)
	}

	if err := createHooksConfigFile(hooksPath); err != nil {
		return fmt.Errorf("failed to create hooks config file: %w", err)
	}

	if err := createAgentsConfigFile(agentsPath); err != nil {
		return fmt.Errorf("failed to create agents config file: %w", err)
	}

	if err := createSkillsDir(skillsDirPath); err != nil {
		return fmt.Errorf("failed to create skills directory: %w", err)
	}

	// --- Create userspace-only files (always ~/.infer/, only-if-absent) ---
	kbCreated, err := createFileIfAbsent(keybindingsPath, func(p string) error {
		return createKeybindingsConfigFile(p)
	})
	if err != nil {
		return fmt.Errorf("failed to create keybindings config file: %w", err)
	}

	remindersCreated, err := createFileIfAbsent(remindersPath, func(p string) error {
		return createRemindersConfigFile(p)
	})
	if err != nil {
		return fmt.Errorf("failed to create reminders config file: %w", err)
	}

	migrated, err := createChannelsConfigFile(channelsPath)
	if err != nil {
		return fmt.Errorf("failed to create channels config file: %w", err)
	}
	channelsCreated := !fileExists(channelsPath) || migrated

	hbCreated, err := createFileIfAbsent(heartbeatPath, func(p string) error {
		return createHeartbeatConfigFile(p)
	})
	if err != nil {
		return fmt.Errorf("failed to create heartbeat config file: %w", err)
	}

	cuMigrated, err := createComputerUseConfigFile(computerUsePath)
	if err != nil {
		return fmt.Errorf("failed to create computer_use config file: %w", err)
	}
	computerUseCreated := !fileExists(computerUsePath) || cuMigrated

	memoryCreated, err := createMemoryConfigFile(memoryConfigPath)
	if err != nil {
		return fmt.Errorf("failed to create memory config file: %w", err)
	}

	// --- .env.example (project only) ---
	envExamplePath := envExampleFileName
	envExampleCreated := false
	if project {
		envExampleCreated = createProjectEnvExample()
	}

	// --- Output ---
	scopeDesc := "userspace"
	if project {
		scopeDesc = "project"
	}

	fmt.Printf("%s Successfully initialized Inference Gateway CLI %s configuration\n", icons.CheckMarkStyle.Render(icons.CheckMark), scopeDesc)
	fmt.Printf("   Created: %s\n", configPath)
	fmt.Printf("   Created: %s\n", gitignorePath)
	fmt.Printf("   Created: %s\n", scmShortcutsPath)
	fmt.Printf("   Created: %s\n", gitShortcutsPath)
	fmt.Printf("   Created: %s\n", mcpShortcutsPath)
	fmt.Printf("   Created: %s\n", shellsShortcutsPath)
	fmt.Printf("   Created: %s\n", exportShortcutsPath)
	fmt.Printf("   Created: %s\n", a2aShortcutsPath)
	fmt.Printf("   Created: %s\n", skillsShortcutsPath)
	fmt.Printf("   Created: %s\n", mcpPath)
	if kbCreated {
		fmt.Printf("   Created: %s\n", keybindingsPath)
	}
	fmt.Printf("   Created: %s\n", promptsPath)
	if remindersCreated {
		fmt.Printf("   Created: %s\n", remindersPath)
	}
	fmt.Printf("   Created: %s\n", hooksPath)
	if channelsCreated {
		fmt.Printf("   Created: %s\n", channelsPath)
	}
	if hbCreated {
		fmt.Printf("   Created: %s\n", heartbeatPath)
	}
	if computerUseCreated {
		fmt.Printf("   Created: %s\n", computerUsePath)
	}
	fmt.Printf("   Created: %s\n", agentsPath)
	fmt.Printf("   Created: %s/\n", skillsDirPath)
	if memoryCreated {
		fmt.Printf("   Created: %s\n", memoryConfigPath)
	}
	if envExampleCreated {
		fmt.Printf("   Created: %s\n", envExamplePath)
	}
	if migrated {
		fmt.Printf("\n%s Migrated legacy `channels:` block from config.yaml into %s.\n", icons.CheckMarkStyle.Render(icons.CheckMark), channelsPath)
		fmt.Printf("   You can now remove the `channels:` block from %s.\n", configPath)
	}
	if cuMigrated {
		fmt.Printf("\n%s Migrated legacy `computer_use:` block from config.yaml into %s.\n", icons.CheckMarkStyle.Render(icons.CheckMark), computerUsePath)
		fmt.Printf("   You can now remove the `computer_use:` block from %s.\n", configPath)
	}
	fmt.Println("")
	if project {
		fmt.Println("This project configuration overrides your userspace baseline (~/.infer/).")
		fmt.Println("Only the settings you include here take effect; everything else is inherited.")
		fmt.Println("")
	} else {
		fmt.Println("This userspace configuration is the shared baseline for all your projects.")
		fmt.Println("Run 'infer init --project' in a repo to add project-specific overrides.")
		fmt.Println("")
	}
	fmt.Println("You can now customize the configuration:")
	fmt.Println("  - Set default model: infer config set agent.model <model-name>")
	fmt.Println("  - Configure tools: infer config tools --help")
	fmt.Println("  - Customize shortcuts: Edit .infer/shortcuts/scm.yaml or add your own")
	fmt.Println("  - Start chatting: infer chat")
	fmt.Println("")
	fmt.Println("Tip: Use /init in chat mode to generate an AGENTS.md file interactively")

	if !skipMigrations {
		handleMigrations()
	}

	return nil
}

// createProjectEnvExample writes .env.example and registers it in .gitignore
// when it does not already exist, returning whether a new file was created.
// It is best-effort - failures print a warning but never abort init - and is
// only invoked in --project mode, where a local .env.example is useful
// scaffolding for per-project secrets.
func createProjectEnvExample() bool {
	if _, err := os.Stat(envExampleFileName); !os.IsNotExist(err) {
		return false
	}

	if err := os.WriteFile(envExampleFileName, []byte(envExampleContent()), 0644); err != nil {
		fmt.Printf("%s Warning: failed to create %s: %v\n", icons.CrossMarkStyle.Render(icons.CrossMark), envExampleFileName, err)
		return false
	}

	if err := ensureEnvInGitignore(); err != nil {
		fmt.Printf("%s Warning: failed to add .env to .gitignore: %v\n", icons.CrossMarkStyle.Render(icons.CrossMark), err)
	}

	return true
}

// createSparseConfigScaffold writes a minimal config.yaml that signals it is a
// project-level override. Settings in this file merge onto ~/.infer/config.yaml
// key-by-key, so only the keys a project actually overrides need to be present.
func createSparseConfigScaffold(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	content := `---
# Project-level configuration overrides.
# Settings here merge onto ~/.infer/config.yaml key-by-key.
# Only include the keys you want to override; everything else
# is inherited from your userspace baseline.
#
# Example:
#   agent:
#     model: anthropic/claude-sonnet-4-20250514
`
	return os.WriteFile(path, []byte(content), 0o644)
}

// fileExists reports whether a path exists on disk.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// createFileIfAbsent runs fn(path) only when the file does not yet exist,
// returning whether a new file was written. Used for userspace-only files
// that may already be present from a prior home init.
func createFileIfAbsent(path string, fn func(string) error) (bool, error) {
	if fileExists(path) {
		return false, nil
	}
	return true, fn(path)
}

// checkFileExists checks if a file exists and returns an error if it does
func checkFileExists(path, description string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s %s already exists (use --overwrite to replace)", description, path)
	}
	return nil
}

// validateFilesNotExist validates that required files do not exist
func validateFilesNotExist(paths ...string) error {
	descriptions := []string{"configuration file", ".gitignore file", "shortcuts file"}
	for i, path := range paths {
		desc := "file"
		if i < len(descriptions) {
			desc = descriptions[i]
		}
		if err := checkFileExists(path, desc); err != nil {
			return err
		}
	}
	return nil
}

// createSCMShortcutsFile creates the SCM shortcuts YAML file
func createSCMShortcutsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create shortcuts directory: %w", err)
	}

	scmShortcutsContent := `# SCM (Source Control Management) Shortcuts
# These shortcuts provide convenient access to GitHub functionality via the gh CLI.
#
# Requirements:
# - GitHub CLI (gh) must be installed: https://cli.github.com
# - Authenticate with: gh auth login
#
# Usage:
# - /scm issues - List all GitHub issues for the repository
# - /scm issue - Show details for a specific GitHub issue
# - /scm pr-create - Generate AI-powered PR plan with branch name, commit, and description

shortcuts:
  - name: scm
    description: "Source control management operations"
    command: gh
    subcommands:
      - name: issues
        description: "List all GitHub issues for the repository"
        args:
          - issue
          - list
          - --json
          - "number,title,state,author,labels,createdAt,updatedAt"
          - --limit
          - "20"

      - name: issue
        description: "Show details for a specific GitHub issue (usage: <number>)"
        args:
          - issue
          - view
          - --json
          - "number,title,body,state,author,labels,comments,createdAt,updatedAt"

      - name: pr-create
        description: "Generate AI-powered PR plan with LLM (usage: [optional context])"
        command: bash
        args:
          - -c
          - |
            diff=$(git diff --cached 2>/dev/null || git diff 2>/dev/null)
            if [ -z "$diff" ]; then
              echo '{"error": "No changes detected. Stage your changes with git add first."}'
              exit 1
            fi
            branch=$(git branch --show-current)
            base_branch="main"
            user_context="$*"
            jq -n \
              --arg diff "$diff" \
              --arg branch "$branch" \
              --arg base "$base_branch" \
              --arg context "$user_context" \
              '{diff: $diff, currentBranch: $branch, baseBranch: $base, userContext: $context}'
          - --
        snippet:
          prompt: |
            Analyze this git diff and generate a step-by-step plan to create a pull request.

            Current branch: {currentBranch}
            Base branch: {baseBranch}

            {userContext}

            Changes:
            ` + "```diff\n            {diff}\n            ```" + `

            Based on the current branch, generate these actions:

            IF current branch is "main" or "master":
              1. Create a new branch with a descriptive name
              2. Stage and commit the changes
              3. Push the branch to remote
              4. Create a pull request with the gh CLI (gh pr create)

            IF current branch is already a feature branch (not main/master):
              1. Stage and commit the changes
              2. Push the branch to remote
              3. Create a pull request with the gh CLI (gh pr create)

            REQUIREMENTS:
            - Branch name: Use conventional format (feat/, fix/, docs/, refactor/, chore/) with kebab-case
            - Commit message: Follow conventional commits format "type: description" (under 50 chars)
            - PR title: Clear and descriptive (similar to commit message but can be slightly longer)
            - PR description: Brief summary of changes (2-3 sentences, focus on WHAT changed and WHY)
            - Use simple, direct language - NO filler words like "comprehensive", "enhance", "robust"
            - For creating the PR, use the gh CLI via the Bash tool (e.g. gh pr create --title "..." --body "...")
            - If user provided additional context, incorporate it into your understanding of the changes

            Output a clear, numbered action plan. Be specific about branch names, commit messages, and PR details based on the diff.
          template: |
            ## Pull Request Plan

            {llm}

            **Next:** I'll help you execute these steps. Let me know when you're ready to proceed.
`

	return os.WriteFile(path, []byte(scmShortcutsContent), 0644)
}

// createGitShortcutsFile creates the Git shortcuts YAML file
func createGitShortcutsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create shortcuts directory: %w", err)
	}

	gitShortcutsContent := `# Git Shortcuts
# Common git operations with AI-powered commit messages
#
# Usage:
# - /git status - Show working tree status
# - /git pull - Pull changes from remote
# - /git push - Push commits to remote
# - /git log - Show commit logs
# - /git commit - Generate AI commit message from staged changes

shortcuts:
  - name: git
    description: "Common git operations"
    command: git
    subcommands:
      - name: status
        description: "Show working tree status"
      - name: pull
        description: "Pull changes from remote repository"
      - name: push
        description: "Push commits to remote repository"
      - name: log
        description: "Show commit logs (last 5)"
        args:
          - --oneline
          - -n
          - "5"
      - name: commit
        description: "Generate AI commit message from staged changes"
        command: bash
        args:
          - -c
          - |
            if ! git diff --cached --quiet 2>/dev/null; then
              diff=$(git diff --cached)
              jq -n --arg diff "$diff" '{diff: $diff}'
            else
              echo '{"error": "No staged changes found. Use git add to stage changes first."}'
              exit 1
            fi
        snippet:
          prompt: |
            Generate a concise git commit message following conventional commit format.

            REQUIREMENTS:
            - MUST use format: "type: Brief description"
            - Type MUST be one of: feat, fix, docs, style, refactor, test, chore
            - Description MUST start with a lowercase letter
            - Description MUST be under 50 characters
            - DO NOT include any explanation, body, or additional text
            - Output ONLY the commit message, nothing else

            Examples of GOOD commit messages:
            - feat: add user authentication
            - fix: resolve memory leak in parser
            - docs: update API documentation
            - refactor: simplify error handling

            Examples of BAD commit messages (DO NOT DO THIS):
            - Add user authentication (missing type)
            - feat: Add user authentication (lowercase description)
            - feat: Added a comprehensive user authentication system with OAuth2 support (too long, too detailed)

            Analyze this diff and generate ONE commit message:

            ` + "```diff\n            {diff}\n            ```" + `

            Output ONLY the commit message in the format "type: description"
          template: "!git commit -m \"{llm}\""
`

	return os.WriteFile(path, []byte(gitShortcutsContent), 0644)
}

// createKeybindingsConfigFile writes a fresh keybindings.yaml seeded from the
// in-code defaults. Generating from DefaultKeybindings() (rather than a
// hardcoded YAML blob) keeps the file in sync as new actions are added.
func createKeybindingsConfigFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return config.SaveKeybindings(path, config.DefaultKeybindingsConfig())
}

// createPromptsConfigFile writes a fresh prompts.yaml seeded from the
// in-code defaults so users can edit individual prompts without touching
// config.yaml.
func createPromptsConfigFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return config.SavePrompts(path, config.DefaultPromptsConfig())
}

// createRemindersConfigFile writes a fresh reminders.yaml seeded from the
// in-code defaults (disabled, one todo-hygiene reminder). Reminders attach to
// the agent-loop hook-point catalog; the companion executable hooks (#270) get
// their own hooks.yaml.
func createRemindersConfigFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return config.SaveReminders(path, config.DefaultRemindersConfig())
}

// createHooksConfigFile writes a fresh hooks.yaml (disabled, with a commented-out
// example). It writes the DefaultHooksYAML template verbatim rather than
// marshalling DefaultHooksConfig so the example and guidance comments survive -
// no hook ships active, since `infer` drives any project, not just Go ones.
func createHooksConfigFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return os.WriteFile(path, []byte(config.DefaultHooksYAML), 0o644)
}

// createChannelsConfigFile writes a fresh channels.yaml. Returns true when
// the file was seeded from a legacy `channels:` block found in viper (i.e.
// migrated from config.yaml) rather than from in-code defaults. Migration
// only runs when no channels.yaml exists yet, so it is safe to re-run init.
func createChannelsConfigFile(path string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, fmt.Errorf("failed to create config directory: %w", err)
	}

	channelsCfg := config.DefaultChannelsConfig()
	migrated := false

	if _, err := os.Stat(path); os.IsNotExist(err) && V != nil && V.IsSet("channels") {
		legacy := config.DefaultChannelsConfig()
		if err := V.UnmarshalKey("channels", legacy); err == nil {
			channelsCfg = legacy
			migrated = true
		}
	}

	if err := config.SaveChannels(path, channelsCfg); err != nil {
		return false, err
	}
	return migrated, nil
}

// createHeartbeatConfigFile writes a fresh heartbeat.yaml seeded from
// the in-code defaults (disabled, hourly interval). Heartbeat is a new
// feature with no legacy config block to migrate, so this is the
// simpler one-step "create from defaults" pattern.
func createHeartbeatConfigFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	return config.SaveHeartbeat(path, config.DefaultHeartbeatConfig())
}

// createComputerUseConfigFile writes a fresh computer_use.yaml. Returns
// true when the file was seeded from a legacy `computer_use:` block found
// in viper (i.e. migrated from config.yaml) rather than from in-code
// defaults. Migration only runs when no computer_use.yaml exists yet, so
// it is safe to re-run init.
func createComputerUseConfigFile(path string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, fmt.Errorf("failed to create config directory: %w", err)
	}

	cuCfg := config.DefaultComputerUseConfig()
	migrated := false

	if _, err := os.Stat(path); os.IsNotExist(err) && V != nil && V.IsSet("computer_use") {
		legacy := config.DefaultComputerUseConfig()
		if err := V.UnmarshalKey("computer_use", legacy); err == nil {
			cuCfg = legacy
			migrated = true
		}
	}

	if err := config.SaveComputerUse(path, cuCfg); err != nil {
		return false, err
	}
	return migrated, nil
}

// createAgentsConfigFile writes a fresh agents.yaml seeded from the in-code
// defaults so users can manage A2A agents via `infer agents` commands.
func createAgentsConfigFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return config.SaveAgents(path, config.DefaultAgentsConfig())
}

// createSkillsDir creates an empty skills directory. Skills are authored by
// dropping a folder containing SKILL.md into this directory; see docs/skills.md for the format.
func createSkillsDir(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create skills directory: %w", err)
	}
	return nil
}

// createMemoryConfigFile seeds ~/.infer/memory.yaml from the in-code defaults
// (enabled by default) when it does not already exist, returning whether a new file was
// written. Memory is global, so its config lives in the home directory and is
// never clobbered by re-running init - that would otherwise reset a user's
// enabled memory from an unrelated project init. The memory store itself
// (MEMORY.md plus per-fact files) is created lazily by the Memory tool on first
// write, so init only seeds the config knob - it does not touch the memory dir.
func createMemoryConfigFile(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := config.SaveMemory(path, config.DefaultMemoryConfig()); err != nil {
		return false, err
	}
	return true, nil
}

// createMCPConfigFile creates the MCP configuration YAML file
func createMCPConfigFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	mcpConfigContent := `---
enabled: false
connection_timeout: 30
discovery_timeout: 30
liveness_probe_enabled: true
liveness_probe_interval: 10
max_retries: 10
servers: []
`

	return os.WriteFile(path, []byte(mcpConfigContent), 0644)
}

// createMCPShortcutsFile creates the MCP shortcuts YAML file
func createMCPShortcutsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create shortcuts directory: %w", err)
	}

	mcpShortcutsContent := `---
# MCP (Model Context Protocol) Shortcuts
# Manage MCP server configuration from within chat
#
# Usage:
# - /mcp list - List all configured MCP servers with details
# - /mcp add - Add a new MCP server
# - /mcp remove - Remove an MCP server
# - /mcp enable - Enable an MCP server
# - /mcp disable - Disable an MCP server
# - /mcp enable-global - Enable MCP globally
# - /mcp disable-global - Disable MCP globally

shortcuts:
  - name: mcp
    description: "Manage MCP servers"
    command: infer
    args:
      - mcp
    subcommands:
      - name: list
        description: "List all configured MCP servers"
      - name: add
        description: "Add a new MCP server (usage: <name> <url> [options])"
      - name: remove
        description: "Remove an MCP server (usage: <name>)"
      - name: enable
        description: "Enable an MCP server (usage: <name>)"
      - name: disable
        description: "Disable an MCP server (usage: <name>)"
      - name: enable-global
        description: "Enable MCP globally"
      - name: disable-global
        description: "Disable MCP globally"
`

	return os.WriteFile(path, []byte(mcpShortcutsContent), 0644)
}

// createShellsShortcutsFile creates the Shells shortcuts YAML file
func createShellsShortcutsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create shortcuts directory: %w", err)
	}

	shellsShortcutsContent := `# Background Shells Shortcuts
# List and manage background shell processes
#
# Usage:
# - /shells - List all background shells

shortcuts:
  - name: shells
    description: "List all running and recent background shell processes"
    tool: ListShells
    tool_args: {}
`

	return os.WriteFile(path, []byte(shellsShortcutsContent), 0644)
}

// createExportShortcutsFile creates the Export shortcuts YAML file
func createExportShortcutsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create shortcuts directory: %w", err)
	}

	exportShortcutsContent := `---
# Export Shortcuts
# Export conversations to markdown files
#
# Usage:
# - /export - Export the current active conversation

shortcuts:
  - name: export
    description: "Export current conversation to markdown"
    command: infer
    args:
      - export
    pass_session_id: true
`

	return os.WriteFile(path, []byte(exportShortcutsContent), 0644)
}

// createEnvShortcutsFile creates the Env shortcuts YAML file that wraps `infer env`,
// so typing `/env` in chat mode runs the env command.
func createEnvShortcutsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create shortcuts directory: %w", err)
	}

	envShortcutsContent := `---
# Env Shortcuts
# Generate a .env.example file with all provider API environment variables
#
# Usage:
# - /env - Generate a .env.example file with provider API keys

shortcuts:
  - name: env
    description: "Generate a .env.example file with provider API keys"
    command: infer
    args:
      - env
`

	return os.WriteFile(path, []byte(envShortcutsContent), 0644)
}

// createA2AShortcutsFile creates the A2A shortcuts YAML file
func createA2AShortcutsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create shortcuts directory: %w", err)
	}

	a2aShortcutsContent := `---
# A2A (Agent-to-Agent) Shortcuts
# Manage A2A agent configuration from within chat
#
# Usage:
# - /agents list - List all configured A2A agents
# - /agents add - Add a new A2A agent
# - /agents remove - Remove an A2A agent
# - /agents enable - Enable an A2A agent
# - /agents disable - Disable an A2A agent

shortcuts:
  - name: agents
    description: "Manage A2A agents"
    command: infer
    args:
      - agents
    subcommands:
      - name: list
        description: "List all configured A2A agents"
      - name: add
        description: "Add a new A2A agent (usage: <name> [url] [options])"
      - name: remove
        description: "Remove an A2A agent (usage: <name>)"
      - name: enable
        description: "Enable an A2A agent (usage: <name>)"
      - name: disable
        description: "Disable an A2A agent (usage: <name>)"
`

	return os.WriteFile(path, []byte(a2aShortcutsContent), 0644)
}

// createSkillsShortcutsFile creates the Agent Skills shortcuts YAML file
func createSkillsShortcutsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create shortcuts directory: %w", err)
	}

	skillsShortcutsContent := `---
# Agent Skills Shortcuts
# Manage Agent Skills from within chat
#
# Usage:
# - /skills list - List discovered skills
# - /skills install <skill | org/skill | github-url> - Install a skill
# - /skills uninstall <name> - Uninstall a skill by name

shortcuts:
  - name: skills
    description: "Manage Agent Skills"
    command: infer
    args:
      - skills
    subcommands:
      - name: list
        description: "List discovered skills"
      - name: install
        description: "Install a skill (usage: <skill> | <org>/<skill> | <github-url>) [--user] [--overwrite]"
      - name: uninstall
        description: "Uninstall a skill by name (usage: <name> [--user])"
`

	return os.WriteFile(path, []byte(skillsShortcutsContent), 0644)
}

// handleMigrations handles the migration logic for the init command
func handleMigrations() {
	defaultConfig := config.DefaultConfig()
	requiresMigrations := defaultConfig.Storage.Type == config.StorageTypeSQLite || defaultConfig.Storage.Type == config.StorageTypePostgres

	if !requiresMigrations {
		fmt.Println("")
		fmt.Printf("%s Storage type '%s' does not require migrations\n", icons.CheckMarkStyle.Render(icons.CheckMark), defaultConfig.Storage.Type)
		return
	}

	fmt.Println("")
	fmt.Println("Running database migrations...")
	if err := runMigrations(); err != nil {
		fmt.Printf("%s Warning: Failed to run migrations: %v\n", icons.CrossMarkStyle.Render(icons.CrossMark), err)
		fmt.Println("   You can run migrations manually with: infer migrate")
	} else {
		fmt.Printf("%s Database migrations completed successfully\n", icons.CheckMarkStyle.Render(icons.CheckMark))
	}
}
