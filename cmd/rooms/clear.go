package rooms

import (
	"context"
	"errors"
	"fmt"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	daemonhost "github.com/inference-gateway/cli/internal/daemon"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	roomscontext "github.com/inference-gateway/cli/internal/rooms"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
	roomsinfra "github.com/inference-gateway/cli/internal/rooms/infrastructure"
	tools "github.com/inference-gateway/cli/internal/tools"
)

// clearRoom empties a room's log through the running daemon, which tells the
// room's followers, or here when no daemon listens. A daemon that listens but
// does not answer keeps its log, since it owns it.
func (c *command) clearRoom(cmd *cobra.Command, args []string) error {
	name := args[0]
	cfg := c.state.Config()
	ctx, cancel := context.WithTimeout(cmd.Context(), daemonTimeout)
	defer cancel()
	client, err := daemonhost.DialLocal(ctx, cfg, clientKind)
	switch {
	case err == nil:
		defer client.Close()
		err = clearThroughDaemon(ctx, client, name)
	case !errors.Is(err, daemonhost.ErrNoBinding) && daemonhost.Reachable(cfg.Binding().Port):
		err = fmt.Errorf("an infer daemon listens on port %d but did not answer, so #%s was left as it is: %w", cfg.Binding().Port, name, err)
	default:
		logger.Debug("rooms clear here, no daemon listens", "error", err)
		err = clearHere(cfg, name)
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cleared #%s. Its members keep their own sessions.\n", name)
	return nil
}

func clearThroughDaemon(ctx context.Context, client *daemonhost.Client, name string) error {
	if err := client.Send(map[string]string{"type": roomsdomain.FrameClearRoom, "room": name}); err != nil {
		return err
	}
	_, err := client.Next(ctx, roomsdomain.FrameRoomCleared)
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
