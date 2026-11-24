package rooms

import (
	"context"
	"fmt"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	daemonhost "github.com/inference-gateway/cli/internal/daemon"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	roomscontext "github.com/inference-gateway/cli/internal/rooms"
	roomsinfra "github.com/inference-gateway/cli/internal/rooms/infrastructure"
	tools "github.com/inference-gateway/cli/internal/tools"
)

// clearRoom empties a room's log through the running daemon, which tells the
// room's followers, or here when no daemon answers.
func (c *command) clearRoom(cmd *cobra.Command, args []string) error {
	name := args[0]
	ctx, cancel := context.WithTimeout(cmd.Context(), daemonTimeout)
	defer cancel()
	client, err := daemonhost.DialLocal(ctx, c.state.Config(), clientKind)
	if err != nil {
		logger.Debug("rooms clear here, the daemon did not answer", "error", err)
		err = clearHere(c.state.Config(), name)
	} else {
		defer client.Close()
		err = clearThroughDaemon(ctx, client, name)
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cleared #%s. Its members keep their own sessions.\n", name)
	return nil
}

func clearThroughDaemon(ctx context.Context, client *daemonhost.Client, name string) error {
	if err := client.Send(map[string]string{"type": "clear_room", "room": name}); err != nil {
		return err
	}
	_, err := client.Next(ctx, "room_cleared")
	return err
}

// clearHere empties the log of the room as this process resolves it.
func clearHere(cfg *config.Config, name string) error {
	room, ok := roomscontext.Find(roomscontext.Resolve(cfg.Rooms, tools.ProjectAgents), name)
	if !ok {
		return fmt.Errorf("room %q not found", name)
	}
	return roomsinfra.NewJSONLLog().Clear(room)
}
