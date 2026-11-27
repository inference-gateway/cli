package teleport

import (
	"context"
	"encoding/json"
	"errors"

	uuid "github.com/google/uuid"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// Place moves the session to target the way a client's teleport_session does
// and returns once it landed there, or with the reason it did not. It lets a
// part of the daemon place a session it opened before the session's first run.
func (s *Service) Place(ctx context.Context, projectDir, sessionID, target string) error {
	landed := make(chan error, 1)
	go s.startTeleport(placement(landed), clientFrame{
		Type: frameTeleportSession, TeleportID: uuid.NewString(), ProjectDir: projectDir, ID: sessionID, Target: target,
	})
	select {
	case err := <-landed:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// placement is the requester of a Place: it reads the teleport's answer and
// reports the first outcome, the landing or the failure.
type placement chan<- error

func (p placement) Deliver(frame []byte) {
	var answer struct {
		Type    string `json:"type"`
		Stage   string `json:"stage"`
		Message string `json:"message"`
	}
	if json.Unmarshal(frame, &answer) != nil {
		return
	}
	switch {
	case answer.Type == frameSessionTeleported:
		p.report(nil)
	case answer.Type == frameRunError, answer.Type == frameTeleportProgress && answer.Stage == string(agentdomain.TeleportFailed):
		p.report(errors.New(answer.Message))
	}
}

func (p placement) report(err error) {
	select {
	case p <- err:
	default:
	}
}
