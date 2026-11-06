package agui

import (
	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

// The events a Run and the Write functions produce, as a client reads them
// back with Decode and switches on.
type (
	Event                        = aguievents.Event
	RunStartedEvent              = aguievents.RunStartedEvent
	RunFinishedEvent             = aguievents.RunFinishedEvent
	RunErrorEvent                = aguievents.RunErrorEvent
	TextMessageStartEvent        = aguievents.TextMessageStartEvent
	TextMessageContentEvent      = aguievents.TextMessageContentEvent
	TextMessageEndEvent          = aguievents.TextMessageEndEvent
	ReasoningMessageStartEvent   = aguievents.ReasoningMessageStartEvent
	ReasoningMessageContentEvent = aguievents.ReasoningMessageContentEvent
	ReasoningMessageEndEvent     = aguievents.ReasoningMessageEndEvent
	ToolCallStartEvent           = aguievents.ToolCallStartEvent
	ToolCallArgsEvent            = aguievents.ToolCallArgsEvent
	ToolCallEndEvent             = aguievents.ToolCallEndEvent
	ToolCallResultEvent          = aguievents.ToolCallResultEvent
	StateSnapshotEvent           = aguievents.StateSnapshotEvent
	StateDeltaEvent              = aguievents.StateDeltaEvent
	MessagesSnapshotEvent        = aguievents.MessagesSnapshotEvent
	ActivitySnapshotEvent        = aguievents.ActivitySnapshotEvent
	CustomEvent                  = aguievents.CustomEvent
)

// Decode reads one frame back into its typed event, a pointer to one of the
// event types above. A frame that is not an AG-UI event is an error, which is
// how a client tells the lowercase app frames of the binding apart.
func Decode(frame []byte) (Event, error) {
	return aguievents.EventFromJSON(frame)
}
