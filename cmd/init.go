package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
	cobra "github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new project with Inference Gateway CLI",
	Long: `Initialize a new project directory with Inference Gateway CLI configuration.
This creates the .infer directory with configuration file and additional setup files like .gitignore.

This is the recommended command to start working with Inference Gateway CLI in a new project.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return initializeProject(cmd)
	},
}

func init() {
	initCmd.Flags().Bool("overwrite", false, "Overwrite existing files if they already exist")
	initCmd.Flags().Bool("userspace", false, "Initialize configuration in user home directory (~/.infer/)")
	initCmd.Flags().Bool("skip-agents-md", false, "Skip generating AGENTS.md file during initialization")
	rootCmd.AddCommand(initCmd)
}

func initializeProject(cmd *cobra.Command) error {
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	userspace, _ := cmd.Flags().GetBool("userspace")
	skipAgentsMD, _ := cmd.Flags().GetBool("skip-agents-md")

	var configPath, gitignorePath, agentsMDPath string

	if userspace {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get user home directory: %w", err)
		}
		configPath = filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
		gitignorePath = filepath.Join(homeDir, config.ConfigDirName, config.GitignoreFileName)
		agentsMDPath = filepath.Join(homeDir, config.ConfigDirName, "AGENTS.md")
	} else {
		configPath = config.DefaultConfigPath
		gitignorePath = filepath.Join(config.ConfigDirName, config.GitignoreFileName)
		agentsMDPath = "AGENTS.md"
	}

	if !overwrite {
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
		}
		if _, err := os.Stat(gitignorePath); err == nil {
			return fmt.Errorf(".gitignore file %s already exists (use --overwrite to replace)", gitignorePath)
		}
		if !skipAgentsMD {
			if _, err := os.Stat(agentsMDPath); err == nil {
				return fmt.Errorf("AGENTS.md file %s already exists (use --overwrite to replace)", agentsMDPath)
			}
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
`

	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644); err != nil {
		return fmt.Errorf("failed to create .gitignore file: %w", err)
	}

	if !skipAgentsMD {
		if err := generateAgentsMD(agentsMDPath, userspace); err != nil {
			return fmt.Errorf("failed to create AGENTS.md file: %w", err)
		}
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
	if !skipAgentsMD {
		fmt.Printf("   Created: %s\n", agentsMDPath)
	}
	fmt.Println("")
	if userspace {
		fmt.Println("This userspace configuration will be used as a fallback for all projects.")
		fmt.Println("Project-level configurations will take precedence when present.")
		fmt.Println("")
	}
	fmt.Println("You can now customize the configuration:")
	fmt.Println("  • Set default model: infer config agent set-model <model-name>")
	fmt.Println("  • Configure tools: infer config tools --help")
	fmt.Println("  • Start chatting: infer chat")

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

// generateAgentsMD creates an AGENTS.md file based on project analysis
func generateAgentsMD(agentsMDPath string, userspace bool) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	content, err := analyzeProjectForAgents(wd, userspace)
	if err != nil {
		return fmt.Errorf("failed to analyze project: %w", err)
	}

	if err := os.WriteFile(agentsMDPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write AGENTS.md: %w", err)
	}

	return nil
}

// projectResearchSystemPrompt returns the system prompt for project research
func projectResearchSystemPrompt() string {
	return `You are a specialized project analysis agent. Your task is to research and understand a software project to create comprehensive documentation for other AI agents working on this project.

ANALYSIS OBJECTIVES:
- Understand the project's architecture, structure, and technologies
- Identify development workflows, build processes, and testing approaches
- Discover project conventions, coding standards, and patterns
- Extract key configuration files, dependencies, and environment setup
- Document important commands, scripts, and automation tools

OUTPUT FORMAT:
Generate an AGENTS.md file following this structure:

# AGENTS.md

## Project Overview
Brief description of what this project does and its main technologies.

## Architecture & Structure
Key directories, modules, and architectural patterns used.

## Development Environment
- Setup instructions and dependencies
- Required tools and versions
- Environment variables and configuration

## Development Workflow
- Build commands and processes
- Testing procedures and commands  
- Code quality tools (linting, formatting)
- Git workflow and branching strategy

## Key Commands
Essential commands developers use regularly:
- Build: ` + "`command`" + `
- Test: ` + "`command`" + `
- Lint: ` + "`command`" + `
- Run: ` + "`command`" + `

## Testing Instructions
- How to run tests
- Test organization and patterns
- Coverage requirements

## Deployment & Release
- Deployment processes
- Release procedures
- CI/CD pipeline information

## Project Conventions
- Coding standards and style guides
- Naming conventions
- File organization patterns
- Commit message formats

## Important Files & Configurations
Key files that agents should be aware of and their purposes.

RESEARCH APPROACH:
1. Start by reading package.json, go.mod, Cargo.toml, or similar dependency files
2. Look for README files, documentation, and setup guides
3. Examine build scripts, Makefiles, or task runners (package.json scripts, Taskfile.yml, etc.)
4. Check for configuration files (.gitignore, .env examples, config files)
5. Identify testing frameworks and CI/CD configurations
6. Look for code quality tools configurations
7. Examine directory structure and common patterns

IMPORTANT GUIDELINES:
- Be concise but comprehensive - agents need actionable information
- Focus on practical development tasks, not theoretical concepts
- Include specific commands and file paths when relevant
- Prioritize information that helps agents work effectively on the project
- Use clear, direct language without unnecessary elaboration
- If information is missing or unclear, acknowledge it rather than guessing

Your analysis should help other agents quickly understand how to work with this project effectively.`
}

// analyzeProjectForAgents analyzes the current project and generates AGENTS.md content
func analyzeProjectForAgents(projectDir string, userspace bool) (string, error) {
	if userspace {
		return getDefaultAgentsMDContent(), nil
	}

	if V == nil {
		return getDefaultAgentsMDContent(), nil
	}

	cfg, err := getConfigFromViper()
	if err != nil {
		return getDefaultAgentsMDContent(), nil
	}

	if cfg == nil {
		return getDefaultAgentsMDContent(), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cfgCopy := *cfg
	cfgCopy.Agent.SystemPrompt = projectResearchSystemPrompt()

	services := container.NewServiceContainer(&cfgCopy, V)
	agentService := services.GetAgentService()
	if agentService == nil {
		return getDefaultAgentsMDContent(), nil
	}

	request := &domain.AgentRequest{
		RequestID: fmt.Sprintf("agents-md-%d", time.Now().Unix()),
		Model:     getProjectAnalysisModel(),
		Messages: []sdk.Message{
			{
				Role:    "user",
				Content: fmt.Sprintf("Please analyze the project in directory '%s' and generate a comprehensive AGENTS.md file. Use your available tools to examine the project structure, configuration files, documentation, build systems, and development workflow. Focus on creating actionable documentation that will help other AI agents understand how to work effectively with this project.", projectDir),
			},
		},
	}

	response, err := agentService.Run(ctx, request)
	if err != nil {
		return getDefaultAgentsMDContent(), nil
	}

	if response == nil || response.Content == "" {
		return getDefaultAgentsMDContent(), nil
	}

	return response.Content, nil
}

// getProjectAnalysisModel returns the model to use for project analysis
func getProjectAnalysisModel() string {
	if model := os.Getenv("INFER_AGENT_MODEL"); model != "" {
		return model
	}
	return "anthropic/claude-3-haiku"
}

// getDefaultAgentsMDContent returns default AGENTS.md content when analysis fails
func getDefaultAgentsMDContent() string {
	return `# AGENTS.md

## Project Overview
This project uses the Inference Gateway CLI for AI-powered development workflows.

## Development Environment
- Ensure you have the Inference Gateway CLI configured
- Check project-specific requirements in README.md or documentation
- Configure your development environment according to project standards

## Development Workflow
- Initialize the project with proper configuration
- Follow established coding patterns and conventions
- Run tests before committing changes
- Use appropriate build and deployment processes

## Key Commands
Check the following files for project-specific commands:
- package.json scripts
- Makefile targets  
- Taskfile.yml tasks
- README.md instructions

## Testing Instructions
- Locate and examine test files in the project
- Run test suites using project-specific test runners
- Ensure all tests pass before submitting changes

## Project Conventions
- Follow the coding style established in the codebase
- Respect existing architecture patterns
- Use consistent naming conventions
- Follow project-specific commit message formats

## Important Files & Configurations
- Configuration files in project root
- Environment variable templates (.env.example)
- Build and deployment scripts
- Documentation files

*This AGENTS.md was generated automatically. For more specific project information, examine the codebase directly or refer to project documentation.*
`
}
