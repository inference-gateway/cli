package daemon

import (
	"errors"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// TestRoomsSettingsFollowRoomsYaml pins that the daemon's rooms read the
// config afresh on every use, so an edit lands without a restart, and keep
// the last rooms that loaded while the config does not.
func TestRoomsSettingsFollowRoomsYaml(t *testing.T) {
	startup := config.RoomsConfig{Rooms: []config.RoomConfig{{Name: "public", Mode: "auto"}}}
	edited := &config.Config{Rooms: config.RoomsConfig{Rooms: []config.RoomConfig{{Name: "public", Mode: "standard"}}}}
	var loadErr error
	settings := roomsSettings(startup, func() (*config.Config, error) {
		if loadErr != nil {
			return nil, loadErr
		}
		return edited, nil
	})

	if mode := settings().Rooms[0].Mode; mode != "standard" {
		t.Fatalf("mode = %q, want the edit read on the next use", mode)
	}
	loadErr = errors.New("rooms.yaml: mapping values are not allowed here")
	if mode := settings().Rooms[0].Mode; mode != "standard" {
		t.Fatalf("mode = %q, want the last rooms that loaded", mode)
	}

	broken := roomsSettings(startup, func() (*config.Config, error) { return nil, loadErr })
	if mode := broken().Rooms[0].Mode; mode != "auto" {
		t.Fatalf("mode = %q, want the startup rooms when nothing loaded since", mode)
	}
}
