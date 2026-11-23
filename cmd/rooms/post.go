package rooms

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	daemonhost "github.com/inference-gateway/cli/internal/daemon"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

// clientKind is what infer rooms post declares in its hello.
const clientKind = "cli"

// postTimeout bounds a post's wait on the daemon, whose router may ask the
// decision model first.
const postTimeout = 2 * time.Minute

func (c *command) postRoom(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), postTimeout)
	defer cancel()
	client, err := daemonhost.DialLocal(ctx, c.state.Config(), clientKind)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Send(map[string]string{"type": "post_room", "room": args[0], "text": args[1]}); err != nil {
		return err
	}
	frame, err := client.Next(ctx, "room_posted")
	if err != nil {
		return err
	}
	var posted roomsdomain.Posted
	if err := decodeFrame(frame, &posted); err != nil {
		return err
	}
	if format, _ := cmd.Flags().GetString("format"); format == "json" {
		return output.PrintJSON(cmd.OutOrStdout(), posted)
	}
	printPosted(cmd.OutOrStdout(), posted)
	return nil
}

// decodeFrame decodes one daemon frame into v.
func decodeFrame(frame map[string]any, v any) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// printPosted prints where a post went, then the notices for its author.
func printPosted(w io.Writer, posted roomsdomain.Posted) {
	_, _ = fmt.Fprintf(w, "Posted to %s.\n", posted.Room)
	_, _ = fmt.Fprintln(w, posted.RouteSummary())
	for _, notice := range posted.Notices {
		_, _ = fmt.Fprintln(w, notice)
	}
}
