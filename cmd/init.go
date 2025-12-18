package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	config "github.com/inference-gateway/cli/config"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	cobra "github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new project with Inference Gateway CLI",
	Long: `Initialize a new project directory with Inference Gateway CLI configuration.
This creates the .infer directory with configuration file and additional setup files like .gitignore.

To generate an AGENTS.md file, use the /init shortcut in interactive chat mode,
which allows you to see the agent's analysis in real-time.

This is the recommended command to start working with Inference Gateway CLI in a new project.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return initializeProject(cmd)
	},
}

func init() {
	initCmd.Flags().Bool("overwrite", false, "Overwrite existing files if they already exist")
	initCmd.Flags().Bool("userspace", false, "Initialize configuration in user home directory (~/.infer/)")
	initCmd.Flags().Bool("skip-migrations", false, "Skip running database migrations")
	rootCmd.AddCommand(initCmd)
}

func initializeProject(cmd *cobra.Command) error {
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	userspace, _ := cmd.Flags().GetBool("userspace")
	skipMigrations, _ := cmd.Flags().GetBool("skip-migrations")

	var configPath, gitignorePath, scmShortcutsPath, gitShortcutsPath, mcpShortcutsPath, shellsShortcutsPath, exportShortcutsPath, a2aShortcutsPath, mcpPath string

	if userspace {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get user home directory: %w", err)
		}
		configPath = filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
		gitignorePath = filepath.Join(homeDir, config.ConfigDirName, config.GitignoreFileName)
		scmShortcutsPath = filepath.Join(homeDir, config.ConfigDirName, "shortcuts", "scm.yaml")
		gitShortcutsPath = filepath.Join(homeDir, config.ConfigDirName, "shortcuts", "git.yaml")
		mcpShortcutsPath = filepath.Join(homeDir, config.ConfigDirName, "shortcuts", "mcp.yaml")
		shellsShortcutsPath = filepath.Join(homeDir, config.ConfigDirName, "shortcuts", "shells.yaml")
		exportShortcutsPath = filepath.Join(homeDir, config.ConfigDirName, "shortcuts", "export.yaml")
		a2aShortcutsPath = filepath.Join(homeDir, config.ConfigDirName, "shortcuts", "a2a.yaml")
		mcpPath = filepath.Join(homeDir, config.ConfigDirName, config.MCPFileName)
	} else {
		configPath = config.DefaultConfigPath
		gitignorePath = filepath.Join(config.ConfigDirName, config.GitignoreFileName)
		scmShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "scm.yaml")
		gitShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "git.yaml")
		mcpShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "mcp.yaml")
		shellsShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "shells.yaml")
		exportShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "export.yaml")
		a2aShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "a2a.yaml")
		mcpPath = filepath.Join(config.ConfigDirName, config.MCPFileName)
	}

	if !overwrite {
		if err := validateFilesNotExist(configPath, gitignorePath, scmShortcutsPath, gitShortcutsPath, mcpShortcutsPath, shellsShortcutsPath, exportShortcutsPath, a2aShortcutsPath, mcpPath); err != nil {
			return err
		}
	}

	if err := writeConfigAsYAMLWithIndent(configPath, 2); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	gitignoreContent := `# Ignore log files and history files
logs/*.log
history
chat_export_*
conversations.db
bin/
tmp/
`

	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644); err != nil {
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

	if err := createA2AShortcutsFile(a2aShortcutsPath); err != nil {
		return fmt.Errorf("failed to create A2A shortcuts file: %w", err)
	}

	if err := createMCPConfigFile(mcpPath); err != nil {
		return fmt.Errorf("failed to create MCP config file: %w", err)
	}

	var scopeDesc string
	if userspace {
		scopeDesc = "userspace"
	} else {
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
	fmt.Printf("   Created: %s\n", mcpPath)
	fmt.Println("")
	if userspace {
		fmt.Println("This userspace configuration will be used as a fallback for all projects.")
		fmt.Println("Project-level configurations will take precedence when present.")
		fmt.Println("")
	}
	fmt.Println("You can now customize the configuration:")
	fmt.Println("  - Set default model: infer config agent set-model <model-name>")
	fmt.Println("  - Configure tools: infer config tools --help")
	fmt.Println("  - Customize shortcuts: Edit .infer/shortcuts/scm.yaml or add your own")
	fmt.Println("  - Start chatting: infer chat")
	fmt.Println("")
	fmt.Println("Tip: Use /init in chat mode to generate an AGENTS.md file interactively")

	// Run database migrations unless skipped
	if !skipMigrations {
		fmt.Println("")
		fmt.Println("Running database migrations...")
		if err := runMigrations(); err != nil {
			fmt.Printf("%s Warning: Failed to run migrations: %v\n", icons.CrossMarkStyle.Render(icons.CrossMark), err)
			fmt.Println("   You can run migrations manually with: infer migrate")
		} else {
			fmt.Printf("%s Database migrations completed successfully\n", icons.CheckMarkStyle.Render(icons.CheckMark))
		}
	}

	return nil
}

// writeConfigAsYAMLWithIndent writes the default configuration to a YAML file with specified indentation
func writeConfigAsYAMLWithIndent(filename string, indent int) error {
	defaultConfig := config.DefaultConfig()

	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	var buf bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&buf)
	yamlEncoder.SetIndent(indent)

	if err := yamlEncoder.Encode(defaultConfig); err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	if err := yamlEncoder.Close(); err != nil {
		return fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	return os.WriteFile(filename, buf.Bytes(), 0644)
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
              4. Create a pull request using the Github tool

            IF current branch is already a feature branch (not main/master):
              1. Stage and commit the changes
              2. Push the branch to remote
              3. Create a pull request using the Github tool

            REQUIREMENTS:
            - Branch name: Use conventional format (feat/, fix/, docs/, refactor/, chore/) with kebab-case
            - Commit message: Follow conventional commits format "type: Description" (under 50 chars, capitalize first letter)
            - PR title: Clear and descriptive (similar to commit message but can be slightly longer)
            - PR description: Brief summary of changes (2-3 sentences, focus on WHAT changed and WHY)
            - Use simple, direct language - NO filler words like "comprehensive", "enhance", "robust"
            - For creating the PR, use the Github tool with resource="create_pull_request"
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
            - Description MUST start with a capital letter
            - Description MUST be under 50 characters
            - DO NOT include any explanation, body, or additional text
            - Output ONLY the commit message, nothing else

            Examples of GOOD commit messages:
            - feat: Add user authentication
            - fix: Resolve memory leak in parser
            - docs: Update API documentation
            - refactor: Simplify error handling

            Examples of BAD commit messages (DO NOT DO THIS):
            - Add user authentication (missing type)
            - feat: add user authentication (lowercase description)
            - feat: added a comprehensive user authentication system with OAuth2 support (too long, too detailed)

            Analyze this diff and generate ONE commit message:

            ` + "```diff\n            {diff}\n            ```" + `

            Output ONLY the commit message in the format "type: Description"
          template: "!git commit -m \"{llm}\""
`

	return os.WriteFile(path, []byte(gitShortcutsContent), 0644)
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
