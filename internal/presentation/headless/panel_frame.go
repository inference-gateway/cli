package headless

import (
	"errors"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// The panel frames a client watches a session's screen with: the latest frame
// of the worker's screen source, which computer_use.screenshot.streaming_enabled
// keeps capturing.
const (
	inboundReadFrame = "read_frame"
	outboundFrame    = "frame"

	screenFrameSource = "screen"
)

// frameFrame answers read_frame with the latest screen frame, its data in
// base64. Unchanged stands in for the frame when the client already has it.
type frameFrame struct {
	Type      string             `json:"type"`
	ID        string             `json:"id"`
	Frame     *agentdomain.Frame `json:"frame,omitempty"`
	Unchanged bool               `json:"unchanged,omitempty"`
	Error     string             `json:"error,omitempty"`
}

// frames answers read_frame from the worker's screen source, looked up by
// name on every request since streaming registers it after the panel starts.
type frames struct {
	write   frameWriter
	sources func(name string) (agentdomain.FrameSource, bool)
}

// read answers with the latest frame, or with unchanged when it is the frame
// the client names as the one it has, so a poll of a still screen stays small.
func (f *frames) read(id, have string) {
	frame, err := f.latest()
	if err != nil {
		f.write(frameFrame{Type: outboundFrame, ID: id, Error: err.Error()})
		return
	}
	if have != "" && frame.ID == have {
		f.write(frameFrame{Type: outboundFrame, ID: id, Unchanged: true})
		return
	}
	f.write(frameFrame{Type: outboundFrame, ID: id, Frame: frame})
}

func (f *frames) latest() (*agentdomain.Frame, error) {
	if f.sources == nil {
		return nil, errors.New("this worker has no screen source")
	}
	source, ok := f.sources(screenFrameSource)
	if !ok {
		return nil, errors.New("this worker streams no screen: enable computer_use.screenshot.streaming_enabled")
	}
	return source.GetLatestFrame()
}
