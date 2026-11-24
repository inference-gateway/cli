package sessions

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	uuid "github.com/google/uuid"
	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	daemonhost "github.com/inference-gateway/cli/internal/daemon"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// NewCommand groups the commands that act on the sessions a daemon hosts. They
// reach each session through the local daemon's binding, wherever it runs.
func NewCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "sessions",
		Short: "Run, stop and teleport the sessions the daemon hosts, and list where they can go",
	}
	command.AddCommand(newWorkspacesCommand(state), newTeleportCommand(state), newRunCommand(state), newStopCommand(state))
	return command
}

// clientKind is what these commands declare in their hello.
const clientKind = "cli"

// clientTimeout bounds one command's wait on the daemon.
const clientTimeout = 30 * time.Minute

// say writes one line of command output, which has nowhere to report a failure.
func say(out io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(out, format, args...)
}

func projectDirFlag(cmd *cobra.Command) (string, error) {
	dir, _ := cmd.Flags().GetString("project-dir")
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func newWorkspacesCommand(state *runtime.State) *cobra.Command {
	return &cobra.Command{
		Use:          "workspaces",
		Short:        "List the workspaces sessions can be teleported to and whether each is reachable",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), clientTimeout)
			defer cancel()
			client, err := daemonhost.DialLocal(ctx, state.Config(), clientKind)
			if err != nil {
				return err
			}
			defer client.Close()
			if err := client.Send(map[string]string{"type": "list_workspaces"}); err != nil {
				return err
			}
			frame, err := client.Next(ctx, "workspaces")
			if err != nil {
				return err
			}
			return printWorkspaces(cmd.OutOrStdout(), frame)
		},
	}
}

func printWorkspaces(out io.Writer, frame map[string]any) error {
	raw, _ := json.Marshal(frame["workspaces"])
	var workspaces []agentdomain.WorkspaceStatus
	if err := json.Unmarshal(raw, &workspaces); err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	say(w, "NAME\tCOMPUTE\tKIND\tPROTOCOL\tREACHABLE\tDETAILS\n")
	for _, workspace := range workspaces {
		details := workspace.Error
		if workspace.Reachable {
			parts := make([]string, 0, len(workspace.Capabilities))
			for _, key := range slices.Sorted(maps.Keys(workspace.Capabilities)) {
				parts = append(parts, key+"="+workspace.Capabilities[key])
			}
			details = strings.Join(parts, " ")
		}
		say(w, "%s\t%s\t%s\t%s\t%t\t%s\n", workspace.Name, cmp.Or(workspace.Compute, "-"), workspace.Kind, workspace.Protocol, workspace.Reachable, details)
	}
	return w.Flush()
}

func newTeleportCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:          "teleport <session-id>",
		Short:        "Teleport a session to another workspace and follow the teleport",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, _ := cmd.Flags().GetString("to")
			if target == "" {
				return errors.New("--to names the workspace to teleport the session to")
			}
			projectDir, err := projectDirFlag(cmd)
			if err != nil {
				return err
			}
			teleportID, _ := cmd.Flags().GetString("teleport-id")
			if teleportID == "" {
				teleportID = uuid.NewString()
			}
			files, _ := cmd.Flags().GetStringSlice("file")
			ctx, cancel := context.WithTimeout(cmd.Context(), clientTimeout)
			defer cancel()
			client, err := daemonhost.DialLocal(ctx, state.Config(), clientKind)
			if err != nil {
				return err
			}
			defer client.Close()
			request := map[string]any{"type": "teleport_session", "teleport_id": teleportID, "project_dir": projectDir, "id": args[0], "target": target, "files": files}
			if err := client.Send(request); err != nil {
				return err
			}
			return followTeleport(ctx, cmd.OutOrStdout(), client)
		},
	}
	command.Flags().String("to", "", "The workspace to teleport the session to (a name from daemon.yaml)")
	command.Flags().String("project-dir", "", "The project the session belongs to (default: the current directory)")
	command.Flags().String("teleport-id", "", "Reuse a teleport id to retry the same teleport idempotently")
	command.Flags().StringSlice("file", nil, "Extra files to carry along, relative to the project (repeatable)")
	return command
}

// followTeleport prints each stage until the teleport is ready or failed.
func followTeleport(ctx context.Context, out io.Writer, client *daemonhost.Client) error {
	for {
		frame, err := client.Next(ctx, "teleport_progress", "session_teleported")
		if err != nil {
			return err
		}
		if frame["type"] == "session_teleported" {
			say(out, "session now lives on %v in %v\n", frame["workspace"], frame["workspace_dir"])
			return nil
		}
		stage, _ := frame["stage"].(string)
		message, _ := frame["message"].(string)
		say(out, "%s %s\n", stage, message)
		if stage == string(agentdomain.TeleportFailed) {
			return errors.New("the teleport failed: " + message)
		}
	}
}

func newRunCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:          "run <session-id> [prompt]",
		Short:        "Continue a session wherever it runs and print the reply, or print its conversation without a prompt",
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir, err := projectDirFlag(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), clientTimeout)
			defer cancel()
			client, err := daemonhost.DialLocal(ctx, state.Config(), clientKind)
			if err != nil {
				return err
			}
			defer client.Close()
			if err := client.Send(map[string]string{"type": "resume_conversation", "project_dir": projectDir, "id": args[0]}); err != nil {
				return err
			}
			snapshot, err := client.Next(ctx, "MESSAGES_SNAPSHOT")
			if err != nil {
				return err
			}
			if len(args) == 1 {
				return printSnapshot(cmd.OutOrStdout(), snapshot)
			}
			return runTurn(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(), client, args[0], args[1])
		},
	}
	command.Flags().String("project-dir", "", "The project the session belongs to (default: the current directory)")
	return command
}

func printSnapshot(out io.Writer, snapshot map[string]any) error {
	raw, _ := json.Marshal(snapshot["messages"])
	var messages []agui.Message
	if err := json.Unmarshal(raw, &messages); err != nil {
		return err
	}
	for _, message := range messages {
		if content, ok := message.Content.(string); ok && content != "" {
			say(out, "%s: %s\n", message.Role, content)
		}
	}
	return nil
}

// runTurn sends one user message and streams the assistant text until the run ends.
func runTurn(ctx context.Context, out, errOut io.Writer, client *daemonhost.Client, conversationID, prompt string) error {
	input := agui.RunAgentInput{ThreadID: conversationID, RunID: uuid.NewString(), Messages: []agui.Message{{ID: uuid.NewString(), Role: "user", Content: prompt}}}
	if err := client.Send(map[string]any{"type": "run_agent_input", "input": input}); err != nil {
		return err
	}
	for {
		frame, err := client.Next(ctx, "TEXT_MESSAGE_CONTENT", "RUN_FINISHED")
		if err != nil {
			return err
		}
		if frame["type"] == "TEXT_MESSAGE_CONTENT" {
			delta, _ := frame["delta"].(string)
			say(out, "%s", delta)
			continue
		}
		say(out, "\n")
		if outcome, ok := frame["outcome"].(map[string]any); ok && outcome["type"] == "interrupt" {
			say(errOut, "the run is waiting on an approval or a question, answer it from a client that follows the thread\n")
		}
		return nil
	}
}

func newStopCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:          "stop <session-id>",
		Short:        "Stop a session's agent and delete its runtime and volumes on the remote workspace that owns it or that it left after teleporting home",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir, err := projectDirFlag(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), clientTimeout)
			defer cancel()
			client, err := daemonhost.DialLocal(ctx, state.Config(), clientKind)
			if err != nil {
				return err
			}
			defer client.Close()
			if err := client.Send(map[string]string{"type": "stop_thread", "project_dir": projectDir, "id": args[0]}); err != nil {
				return err
			}
			frame, err := client.Next(ctx, "thread_stopped")
			if err != nil {
				return err
			}
			return printStopped(cmd.OutOrStdout(), frame)
		},
	}
	command.Flags().String("project-dir", "", "The project the session belongs to (default: the current directory)")
	return command
}

// printStopped reports where the session's runtime was deleted, or why not.
func printStopped(out io.Writer, frame map[string]any) error {
	if message, _ := frame["error"].(string); message != "" {
		return errors.New(message)
	}
	if workspace, _ := frame["workspace"].(string); workspace != "" {
		say(out, "deleted the session's runtime on %s, the session continues on this daemon\n", workspace)
		return nil
	}
	say(out, "stopped the session's worker on this daemon\n")
	return nil
}
