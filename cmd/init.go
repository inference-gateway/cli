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
	rootCmd.AddCommand(initCmd)
}

func initializeProject(cmd *cobra.Command) error {
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	userspace, _ := cmd.Flags().GetBool("userspace")

	var configPath, gitignorePath, scmShortcutsPath, gitShortcutsPath, mcpShortcutsPath, mcpPath string

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
		mcpPath = filepath.Join(homeDir, config.ConfigDirName, config.MCPFileName)
	} else {
		configPath = config.DefaultConfigPath
		gitignorePath = filepath.Join(config.ConfigDirName, config.GitignoreFileName)
		scmShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "scm.yaml")
		gitShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "git.yaml")
		mcpShortcutsPath = filepath.Join(config.ConfigDirName, "shortcuts", "mcp.yaml")
		mcpPath = filepath.Join(config.ConfigDirName, config.MCPFileName)
	}

	if !overwrite {
		if err := validateFilesNotExist(configPath, gitignorePath, scmShortcutsPath, gitShortcutsPath, mcpShortcutsPath, mcpPath); err != nil {
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
# - /scm-issues - List all GitHub issues for the repository
# - /scm-issue <number> - Show details for a specific GitHub issue
# - /scm-pr-create [optional context] - Generate AI-powered PR plan with branch name, commit, and description

shortcuts:
  - name: scm-issues
    description: "List all GitHub issues for the repository"
    command: gh
    args:
      - issue
      - list
      - --json
      - "number,title,state,author,labels,createdAt,updatedAt"
      - --limit
      - "20"

  - name: scm-issue
    description: "Show details for a specific GitHub issue (usage: /scm-issue <number>)"
    command: gh
    args:
      - issue
      - view
      - --json
      - "number,title,body,state,author,labels,comments,createdAt,updatedAt"

  - name: scm-pr-create
    description: "Generate AI-powered PR plan with LLM (usage: /scm-pr-create [optional context])"
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
        ` + "```diff\n        {diff}\n        ```" + `

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
# - /git-status - Show working tree status
# - /git-pull - Pull changes from remote
# - /git-push - Push commits to remote
# - /git-log - Show commit logs
# - /git-commit - Generate AI commit message from staged changes

shortcuts:
  - name: git-status
    description: "Show working tree status"
    command: git
    args:
      - status

  - name: git-pull
    description: "Pull changes from remote repository"
    command: git
    args:
      - pull

  - name: git-push
    description: "Push commits to remote repository"
    command: git
    args:
      - push

  - name: git-log
    description: "Show commit logs (last 10)"
    command: git
    args:
      - log
      - --oneline
      - --graph
      - --decorate
      - "-10"

  - name: git-commit
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

        ` + "```diff\n        {diff}\n        ```" + `

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
# - /mcp - List all configured MCP servers with details
# - /mcp-add <name> <url> [description] - Add a new MCP server
# - /mcp-remove <name> - Remove an MCP server
# - /mcp-enable <name> - Enable an MCP server
# - /mcp-disable <name> - Disable an MCP server
# - /mcp-enable-global - Enable MCP globally
# - /mcp-disable-global - Disable MCP globally

shortcuts:
  - name: mcp
    description: "List all configured MCP servers"
    command: infer
    args:
      - mcp
      - list

  - name: mcp-add
    description: "Add a new MCP server (usage: /mcp-add <name> <url> [description])"
    command: bash
    args:
      - -c
      - |
        if [ $# -lt 2 ]; then
          echo "Usage: /mcp-add <name> <url> [description]"
          echo "Example: /mcp-add filesystem http://localhost:3000/sse File operations"
          exit 1
        fi

        NAME="$1"
        URL="$2"
        shift 2
        DESCRIPTION="$*"

        if [ -n "$DESCRIPTION" ]; then
          infer mcp add "$NAME" "$URL" --description="$DESCRIPTION"
        else
          infer mcp add "$NAME" "$URL"
        fi

  - name: mcp-remove
    description: "Remove an MCP server (usage: /mcp-remove <name>)"
    command: bash
    args:
      - -c
      - |
        if [ $# -lt 1 ]; then
          echo "Usage: /mcp-remove <name>"
          echo "Example: /mcp-remove filesystem"
          exit 1
        fi
        infer mcp remove "$1"

  - name: mcp-enable
    description: "Enable an MCP server (usage: /mcp-enable <name>)"
    command: bash
    args:
      - -c
      - |
        if [ $# -lt 1 ]; then
          echo "Usage: /mcp-enable <name>"
          echo "Example: /mcp-enable filesystem"
          exit 1
        fi
        infer mcp enable "$1"

  - name: mcp-disable
    description: "Disable an MCP server (usage: /mcp-disable <name>)"
    command: bash
    args:
      - -c
      - |
        if [ $# -lt 1 ]; then
          echo "Usage: /mcp-disable <name>"
          echo "Example: /mcp-disable filesystem"
          exit 1
        fi
        infer mcp disable "$1"

  - name: mcp-enable-global
    description: "Enable MCP globally"
    command: infer
    args:
      - mcp
      - enable-global

  - name: mcp-disable-global
    description: "Disable MCP globally"
    command: infer
    args:
      - mcp
      - disable-global
`

	return os.WriteFile(path, []byte(mcpShortcutsContent), 0644)
}
