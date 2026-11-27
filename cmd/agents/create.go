package agents

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	tools "github.com/inference-gateway/cli/internal/tools"
)

func (c *command) newCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name> --role <role>",
		Short: "Create a Markdown agent from a role template",
		Long: `Create a Markdown agent, <name>.md in ~/.infer/agents (or .infer/agents with --project), from the template
of its role: ` + strings.Join(tools.AgentTemplateRoles(), ", ") + ` have one of their own, every other role starts
from the default. The name is the agent's handle: @<name> reaches it in a room and the Agent tool runs it by it.
The role is its job title, shown after the name as "<name> (<role>)".`,
		Example: `  infer agents create jeff --role tester --emoji 🧪 --workspace ubuntu-desktop
  infer agents create noah --role "code reviewer" --model anthropic/claude-sonnet-5-5`,
		Args: cobra.ExactArgs(1),
		RunE: c.createAgent,
	}
	cmd.Flags().String("role", "", "The agent's job title, such as tester or \"code reviewer\" (required)")
	cmd.Flags().String("description", "", "What the agent does and when to hand work to it (default: the template's)")
	cmd.Flags().String("emoji", "", "The face the desktop shows for the agent")
	cmd.Flags().String("model", "", "The model it runs on (format: provider/model, default: inherit)")
	cmd.Flags().String("workspace", "", "The computer it runs on, a workspace of daemon.yaml")
	cmd.Flags().StringSlice("tools", nil, "The tools it may use (default: every tool)")
	cmd.Flags().Bool("force", false, "Overwrite an existing agent file")
	_ = cmd.MarkFlagRequired("role")
	return cmd
}

func (c *command) createAgent(cmd *cobra.Command, args []string) error {
	dir, err := runtime.ConfigPath(runtime.ProjectFlag(cmd), config.MarkdownAgentsDirName)
	if err != nil {
		return err
	}
	spec := tools.AgentSpec{Name: args[0]}
	spec.Role, _ = cmd.Flags().GetString("role")
	spec.Description, _ = cmd.Flags().GetString("description")
	spec.Emoji, _ = cmd.Flags().GetString("emoji")
	spec.Model, _ = cmd.Flags().GetString("model")
	spec.Workspace, _ = cmd.Flags().GetString("workspace")
	spec.Tools, _ = cmd.Flags().GetStringSlice("tools")
	force, _ := cmd.Flags().GetBool("force")
	path, err := tools.WriteAgent(dir, spec, force)
	if errors.Is(err, tools.ErrAgentExists) {
		return fmt.Errorf("%s exists - edit it, or pass --force to start it over from the template", path)
	}
	if err != nil {
		return err
	}
	warnNonBuiltinTools(cmd, spec.Tools)
	fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n  @%s reaches it in a room, and /agents lists it in the chat. Edit the file to make it yours.\n", filepath.Clean(path), spec.Name)
	return nil
}

// warnNonBuiltinTools flags tool names that are not built in, often a typo.
// They still load when a browser, A2A or custom tool provides them, so they warn
// rather than fail.
func warnNonBuiltinTools(cmd *cobra.Command, names []string) {
	builtins := tools.ToolNames()
	for _, name := range names {
		if name = strings.TrimSpace(name); !slices.Contains(builtins, name) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s is not a built-in tool - the agent drops it unless a browser, A2A or custom tool provides it\n", name)
		}
	}
}
