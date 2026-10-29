package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	uuid "github.com/google/uuid"
	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	browserinfra "github.com/inference-gateway/cli/internal/browser/infrastructure"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// clientKind is what these commands declare in their hello.
const clientKind = "cli"

// clientTimeout bounds one command's wait on the daemon.
const clientTimeout = 30 * time.Minute

// daemonClient is one connection of a CLI command to the local daemon.
type daemonClient struct {
	conn   *agui.Conn
	frames chan []byte
	once   sync.Once
}

func (c *daemonClient) Attach(*agui.Conn) {}

func (c *daemonClient) Handle(_ *agui.Conn, frame []byte) {
	c.frames <- append([]byte(nil), frame...)
}

func (c *daemonClient) Detach(*agui.Conn) {
	c.once.Do(func() { close(c.frames) })
}

// dialLocal connects to the daemon this config describes, over its TLS when
// it serves any.
func dialLocal(ctx context.Context, cfg *config.Config) (*daemonClient, error) {
	binding := cfg.Binding()
	if !authenticates(binding) {
		return nil, errors.New("the daemon binding is off or has neither a token nor TLS client certificates: enable daemon.binding in daemon.yaml with a token or a client-certificate TLS mode, and start infer daemon")
	}
	var pki *agentinfra.PKI
	if binding.TLS.Mode == config.TLSModeCA {
		var err error
		if pki, err = agentinfra.LoadOrCreatePKI(filepath.Join(config.UserSpaceConfigDir(), "daemon", "pki")); err != nil {
			return nil, err
		}
	}
	provider := agentinfra.NewTLSProvider(clientKind, pki, binding.TLS.SPIFFE)
	tlsConfig, err := provider.ClientForBinding(ctx, binding.TLS)
	if err != nil {
		return nil, err
	}
	client := &daemonClient{frames: make(chan []byte, 256)}
	conn, err := agui.Dial(ctx, agui.DialConfig{
		Addr: binding.Addr(), Token: binding.Token, Kind: clientKind, Handshake: browserinfra.BindingHandshake, TLS: tlsConfig,
	}, client)
	if err != nil {
		return nil, fmt.Errorf("is infer daemon running? %w", err)
	}
	client.conn = conn
	return client, nil
}

// authenticates reports whether these commands can dial the binding: it is on
// and a token or a client certificate proves who they are.
func authenticates(binding config.Binding) bool {
	return binding.Enabled && (binding.Token != "" || binding.TLS.RequiresClientAuth())
}

func (c *daemonClient) close() {
	c.conn.Close()
}

func (c *daemonClient) send(frame any) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return c.conn.Send(data)
}

// next returns the next frame of one of the types, or the error a RUN_ERROR
// carries.
func (c *daemonClient) next(ctx context.Context, types ...string) (map[string]any, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case raw, ok := <-c.frames:
			if !ok {
				return nil, errors.New("the daemon hung up")
			}
			var frame map[string]any
			if json.Unmarshal(raw, &frame) != nil {
				continue
			}
			typ, _ := frame["type"].(string)
			for _, want := range types {
				if typ == want {
					return frame, nil
				}
			}
			if typ == "RUN_ERROR" {
				message, _ := frame["message"].(string)
				return nil, errors.New(message)
			}
		}
	}
}

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

func newEnvironmentsCommand(state *runtime.State) *cobra.Command {
	return &cobra.Command{
		Use:          "environments",
		Short:        "List the environments sessions can be transferred to and whether each is reachable",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), clientTimeout)
			defer cancel()
			client, err := dialLocal(ctx, state.Config())
			if err != nil {
				return err
			}
			defer client.close()
			if err := client.send(map[string]string{"type": "list_environments"}); err != nil {
				return err
			}
			frame, err := client.next(ctx, "environments")
			if err != nil {
				return err
			}
			return printEnvironments(cmd.OutOrStdout(), frame)
		},
	}
}

func printEnvironments(out io.Writer, frame map[string]any) error {
	raw, _ := json.Marshal(frame["environments"])
	var environments []agentdomain.EnvironmentStatus
	if err := json.Unmarshal(raw, &environments); err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	say(w, "NAME\tKIND\tREACHABLE\tDETAILS\n")
	for _, environment := range environments {
		details := environment.Error
		if environment.Reachable {
			parts := make([]string, 0, len(environment.Capabilities))
			for _, key := range slices.Sorted(maps.Keys(environment.Capabilities)) {
				parts = append(parts, key+"="+environment.Capabilities[key])
			}
			details = strings.Join(parts, " ")
		}
		say(w, "%s\t%s\t%t\t%s\n", environment.Name, environment.Kind, environment.Reachable, details)
	}
	return w.Flush()
}

func newTransferCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:          "transfer <conversation-id>",
		Short:        "Move a session to another environment and follow the transfer",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, _ := cmd.Flags().GetString("to")
			if target == "" {
				return errors.New("--to names the environment to move the session to")
			}
			projectDir, err := projectDirFlag(cmd)
			if err != nil {
				return err
			}
			transferID, _ := cmd.Flags().GetString("transfer-id")
			if transferID == "" {
				transferID = uuid.NewString()
			}
			files, _ := cmd.Flags().GetStringSlice("file")
			ctx, cancel := context.WithTimeout(cmd.Context(), clientTimeout)
			defer cancel()
			client, err := dialLocal(ctx, state.Config())
			if err != nil {
				return err
			}
			defer client.close()
			request := map[string]any{"type": "transfer_session", "transfer_id": transferID, "project_dir": projectDir, "id": args[0], "target": target, "files": files}
			if err := client.send(request); err != nil {
				return err
			}
			return followTransfer(ctx, cmd.OutOrStdout(), client)
		},
	}
	command.Flags().String("to", "", "The environment to move the session to (a name from daemon.yaml)")
	command.Flags().String("project-dir", "", "The project the conversation belongs to (default: the current directory)")
	command.Flags().String("transfer-id", "", "Reuse a transfer id to retry the same transfer idempotently")
	command.Flags().StringSlice("file", nil, "Extra files to carry along, relative to the project (repeatable)")
	return command
}

// followTransfer prints each stage until the transfer is ready or failed.
func followTransfer(ctx context.Context, out io.Writer, client *daemonClient) error {
	for {
		frame, err := client.next(ctx, "transfer_progress", "session_moved")
		if err != nil {
			return err
		}
		if frame["type"] == "session_moved" {
			say(out, "session now lives on %v in %v\n", frame["environment"], frame["workspace"])
			return nil
		}
		stage, _ := frame["stage"].(string)
		message, _ := frame["message"].(string)
		say(out, "%s %s\n", stage, message)
		if stage == string(agentdomain.TransferFailed) {
			return errors.New("the transfer failed: " + message)
		}
	}
}

func newRunCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:          "run <conversation-id> [prompt]",
		Short:        "Continue a conversation through the daemon, wherever its session runs, and print the reply",
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir, err := projectDirFlag(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), clientTimeout)
			defer cancel()
			client, err := dialLocal(ctx, state.Config())
			if err != nil {
				return err
			}
			defer client.close()
			if err := client.send(map[string]string{"type": "resume_conversation", "project_dir": projectDir, "id": args[0]}); err != nil {
				return err
			}
			snapshot, err := client.next(ctx, "MESSAGES_SNAPSHOT")
			if err != nil {
				return err
			}
			if len(args) == 1 {
				return printSnapshot(cmd.OutOrStdout(), snapshot)
			}
			return runTurn(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(), client, args[0], args[1])
		},
	}
	command.Flags().String("project-dir", "", "The project the conversation belongs to (default: the current directory)")
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
func runTurn(ctx context.Context, out, errOut io.Writer, client *daemonClient, conversationID, prompt string) error {
	input := agui.RunAgentInput{ThreadID: conversationID, RunID: uuid.NewString(), Messages: []agui.Message{{ID: uuid.NewString(), Role: "user", Content: prompt}}}
	if err := client.send(map[string]any{"type": "run_agent_input", "input": input}); err != nil {
		return err
	}
	for {
		frame, err := client.next(ctx, "TEXT_MESSAGE_CONTENT", "RUN_FINISHED")
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
		Use:          "stop <conversation-id>",
		Short:        "Stop a session's agent and delete its runtime and volumes on the remote environment that owns it",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir, err := projectDirFlag(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), clientTimeout)
			defer cancel()
			client, err := dialLocal(ctx, state.Config())
			if err != nil {
				return err
			}
			defer client.close()
			if err := client.send(map[string]string{"type": "stop_thread", "project_dir": projectDir, "id": args[0]}); err != nil {
				return err
			}
			frame, err := client.next(ctx, "thread_stopped")
			if err != nil {
				return err
			}
			return printStopped(cmd.OutOrStdout(), frame)
		},
	}
	command.Flags().String("project-dir", "", "The project the conversation belongs to (default: the current directory)")
	return command
}

// printStopped reports where the session's runtime was deleted, or why not.
func printStopped(out io.Writer, frame map[string]any) error {
	if message, _ := frame["error"].(string); message != "" {
		return errors.New(message)
	}
	if environment, _ := frame["environment"].(string); environment != "" {
		say(out, "deleted the session's runtime on %s and released its lease, it continues here next time\n", environment)
		return nil
	}
	say(out, "stopped the session's worker on this daemon\n")
	return nil
}
