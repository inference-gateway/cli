package workflow

import (
	"fmt"
	"os"
	"strings"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	githubsetup "github.com/inference-gateway/cli/internal/github/setup"
)

// NewCommand constructs the workflow command tree.
func NewCommand(state *runtime.State) *cobra.Command {
	workflowCmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage the OpenTask Agent GitHub workflow",
	}

	installCmd := &cobra.Command{
		Use:   "install [owner/repo]",
		Short: "Install or update the OpenTask Agent workflow via an LLM agent",
		Long: `Install or update .github/workflows/tasks.yml in a GitHub repository.

An LLM agent clones the repository, reads the existing workflow (when there is
one) and applies only infer-action-related changes - bumping the action version
and adding missing inputs - while preserving repo-specific customizations such
as build steps, GitHub App tokens, languages, plugins, and agents. It considers
the repository's languages and takes inspiration from an existing CI workflow.

The change lands as a pull request with a Conventional Commit title and a body
describing what changed. Re-running pushes onto the same branch and updates the
open install PR instead of creating a new one.

Examples:
  infer workflow install                       # current repository
  infer workflow install owner/repo
  infer workflow install owner/repo --model anthropic/claude-fable-5
  infer workflow install owner/repo --context "the repo deploys with bun"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := agentdomain.InstallWorkflowOptions{}
			if len(args) == 1 {
				opts.Repo = args[0]
			}
			opts.Model, _ = cmd.Flags().GetString("model")
			if opts.Model == "" {
				opts.Model = state.Config().Agent.Model
			}
			opts.Context, _ = cmd.Flags().GetString("context")
			if contextFile, _ := cmd.Flags().GetString("context-file"); contextFile != "" {
				data, err := os.ReadFile(contextFile)
				if err != nil {
					return fmt.Errorf("read context file: %w", err)
				}
				opts.Context = strings.TrimSpace(opts.Context + "\n" + string(data))
			}
			opts.GitHubApp, _ = cmd.Flags().GetBool("github-app")

			service := githubsetup.NewService(&githubsetup.RealRunner{})
			prURL, err := service.InstallWorkflow(cmd.Context(), opts)
			if err != nil {
				return err
			}
			fmt.Println(prURL)
			return nil
		},
	}
	installCmd.Flags().StringP("model", "m", "", "Model for the install agent and the workflow default (e.g. openai/gpt-4o)")
	installCmd.Flags().String("context", "", "Extra instructions for the install agent")
	installCmd.Flags().String("context-file", "", "File with extra instructions for the install agent")
	installCmd.Flags().Bool("github-app", false, "Use the GitHub App token variant of the workflow")

	workflowCmd.AddCommand(installCmd)
	return workflowCmd
}
