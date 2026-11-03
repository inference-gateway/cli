package infrastructure

import (
	"encoding/json"
	"errors"

	uuid "github.com/google/uuid"

	adk "github.com/inference-gateway/adk/types"
)

// The peer wire: the A2A 1.0 envelopes two infer daemons exchange when a
// workspace serves its teleport vocabulary over A2A JSON-RPC. One exchange is
// one contextId, every frame rides as the text part of a message, and the
// event that announces the host carries metadata and no frame.
const PeerPath = "/a2a"

// PeerMethod is one of the two A2A methods the peer endpoint serves.
type PeerMethod string

const (
	PeerMethodSend   = PeerMethod(adk.A2AMethodSendMessage)
	PeerMethodStream = PeerMethod(adk.A2AMethodSendStreamingMessage)
)

// PeerRequest is one JSON-RPC request the peer endpoint takes: its id to echo,
// the method, the exchange the message's contextId names, the frame in the
// message's first text part and the metadata the caller announced.
type PeerRequest struct {
	ID       adk.Value
	Method   PeerMethod
	Exchange string
	Frame    []byte
	Metadata map[string]string
}

// ParsePeerRequest reads a peer request. A method that is not SendMessage or
// SendStreamingMessage is not ok.
func ParsePeerRequest(body []byte) (PeerRequest, bool) {
	var req adk.JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return PeerRequest{}, false
	}
	method := PeerMethod(req.Method)
	if method != PeerMethodSend && method != PeerMethodStream {
		return PeerRequest{}, false
	}
	var params adk.SendMessageRequest
	raw, _ := json.Marshal(req.Params)
	if err := json.Unmarshal(raw, &params); err != nil {
		return PeerRequest{}, false
	}
	var id adk.Value
	if req.ID != nil {
		id = *req.ID
	}
	frame, metadata := peerMessageContent(params.Message)
	return PeerRequest{ID: id, Method: method, Exchange: contextIDOf(params.Message), Frame: frame, Metadata: metadata}, true
}

// EncodePeerRequest is the request a dialing daemon sends: a user message on
// the exchange carrying the frame, announcing the caller in its metadata.
func EncodePeerRequest(method PeerMethod, exchange string, frame []byte, metadata map[string]string) []byte {
	message := peerMessage(adk.RoleUser, frame, metadata)
	message.ContextID = &exchange
	params := adk.Struct{"message": message}
	id := adk.Value(uuid.NewString())
	raw, _ := json.Marshal(adk.JSONRPCRequest{JSONRPC: "2.0", ID: &id, Method: adk.A2AMethod(method), Params: &params})
	return raw
}

// PeerEvent is one event of a peer stream: an agent message carrying the frame,
// or the announcing one carrying only metadata.
func PeerEvent(frame []byte, metadata map[string]string) []byte {
	message := peerMessage(adk.RoleAgent, frame, metadata)
	raw, _ := json.Marshal(adk.StreamResponse{Message: &message})
	return raw
}

// DecodePeerEvent reads one stream event, returning its frame and metadata.
func DecodePeerEvent(data []byte) (frame []byte, metadata map[string]string, hasFrame bool) {
	var event adk.StreamResponse
	if err := json.Unmarshal(data, &event); err != nil || event.Message == nil {
		return nil, nil, false
	}
	frame, metadata = peerMessageContent(*event.Message)
	return frame, metadata, len(frame) > 0
}

// PeerAck answers a SendMessage with the completed task on the exchange. Its
// artifacts stay empty, because every frame rides the stream.
func PeerAck(id adk.Value, exchange string) []byte {
	task := adk.Task{ID: uuid.NewString(), ContextID: &exchange, Status: adk.TaskStatus{State: adk.TaskStateCompleted}}
	raw, _ := json.Marshal(adk.JSONRPCSuccessResponse{JSONRPC: "2.0", ID: id, Result: adk.SendMessageResponse{Task: &task}})
	return raw
}

// PeerError answers a request the endpoint cannot serve with the invalid
// params error.
func PeerError(id adk.Value, message string) []byte {
	raw, _ := json.Marshal(adk.JSONRPCErrorResponse{JSONRPC: "2.0", ID: id, Error: adk.JSONRPCError{Code: -32602, Message: message}})
	return raw
}

// DecodePeerReply is the error a SendMessage reply carries, nil on an ack.
func DecodePeerReply(body []byte) error {
	var reply adk.JSONRPCErrorResponse
	if err := json.Unmarshal(body, &reply); err != nil || reply.Error.Message == "" {
		return nil
	}
	return errors.New(reply.Error.Message)
}

// PeerCard is the agent card of a peer endpoint served at url.
func PeerCard(url, version string) []byte {
	raw, _ := json.Marshal(adk.AgentCard{
		Name:                "infer",
		Description:         "An infer daemon's peer endpoint: session teleports and relays between workspaces.",
		Version:             version,
		Capabilities:        adk.AgentCapabilities{Streaming: ptr(true)},
		DefaultInputModes:   []string{"application/json"},
		DefaultOutputModes:  []string{"application/json"},
		Skills:              []adk.AgentSkill{{ID: "peer", Name: "peer", Description: "Carries the daemon-to-daemon frame vocabulary.", Tags: []string{"teleport"}}},
		SupportedInterfaces: []adk.AgentInterface{{URL: url, ProtocolBinding: "JSONRPC", ProtocolVersion: adk.A2AProtocolVersion}},
	})
	return raw
}

// SSE renders one event as a server-sent events data line.
func SSE(event []byte) []byte {
	return append(append([]byte("data: "), event...), '\n', '\n')
}

func peerMessage(role adk.Role, frame []byte, metadata map[string]string) adk.Message {
	message := adk.Message{MessageID: uuid.NewString(), Role: role, Parts: []adk.Part{}}
	if len(frame) > 0 {
		text := string(frame)
		message.Parts = []adk.Part{{Text: &text}}
	}
	if len(metadata) > 0 {
		attrs := make(adk.Struct, len(metadata))
		for key, value := range metadata {
			attrs[key] = value
		}
		message.Metadata = &attrs
	}
	return message
}

func peerMessageContent(message adk.Message) (frame []byte, metadata map[string]string) {
	if len(message.Parts) > 0 && message.Parts[0].Text != nil {
		frame = []byte(*message.Parts[0].Text)
	}
	if message.Metadata != nil {
		metadata = make(map[string]string, len(*message.Metadata))
		for key, value := range *message.Metadata {
			if text, ok := value.(string); ok {
				metadata[key] = text
			}
		}
	}
	return frame, metadata
}

func contextIDOf(message adk.Message) string {
	if message.ContextID == nil {
		return ""
	}
	return *message.ContextID
}

func ptr[T any](value T) *T { return &value }
