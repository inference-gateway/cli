package telegram

import (
	"context"
	"strings"
	"testing"

	channelmocks "github.com/inference-gateway/cli/tests/mocks/channels"
	sessionmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	config "github.com/inference-gateway/cli/config"
	channels "github.com/inference-gateway/cli/internal/channels"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// renderFrames drives the chat's render path over raw session-worker
// frames and collects what reaches the chat's user.
func renderFrames(t *testing.T, frames ...string) []channels.OutboundMessage {
	t.Helper()
	cm := NewChannelManagerService(config.ChannelsConfig{Enabled: true}, nil)
	router := &sessionmocks.FakeThreadRouter{}
	cm.SetThreadDriver(router, t.TempDir(), sessionsdomain.ThreadOptions{})
	ch := &channelmocks.FakeChannel{}
	sent := make(chan channels.OutboundMessage, 16)
	ch.NameReturns("telegram")
	ch.SendStub = func(_ context.Context, msg channels.OutboundMessage) error {
		sent <- msg
		return nil
	}
	chat := cm.threadChatFor(context.Background(), "telegram-123", ch, "123")
	for _, frame := range frames {
		chat.render([]byte(frame))
	}
	close(sent)
	var out []channels.OutboundMessage
	for msg := range sent {
		out = append(out, msg)
	}
	return out
}

// RenderText renders one assistant message from its streamed triad, keeping
// the resumed snapshot and the worker's echo of the inbound message off the
// chat.
func TestThreadChat_RenderTextMessage(t *testing.T) {
	sent := renderFrames(t,
		`{"type":"MESSAGES_SNAPSHOT"}`,
		`{"type":"TEXT_MESSAGE_START","role":"user"}`,
		`{"type":"TEXT_MESSAGE_CONTENT","delta":"should not be re-rendered"}`,
		`{"type":"TEXT_MESSAGE_END"}`,
		`{"type":"TEXT_MESSAGE_START","role":"assistant"}`,
		`{"type":"TEXT_MESSAGE_CONTENT","delta":"Hello "}`,
		`{"type":"TEXT_MESSAGE_CONTENT","delta":"world"}`,
		`{"type":"TEXT_MESSAGE_END"}`,
	)
	if len(sent) != 1 {
		t.Fatalf("expected exactly one assistant message, got %v", sent)
	}
	if sent[0].Content != "Hello world" {
		t.Errorf("expected the assembled message, got %q", sent[0].Content)
	}
	if sent[0].RecipientID != "123" {
		t.Errorf("expected recipient 123, got %q", sent[0].RecipientID)
	}
}

// RenderToolCalls delivers the tool call and its failed result as quoted
// blocks, the way the subprocess run forwarded tool traffic, with text
// continuing after the result.
func TestThreadChat_RenderToolCalls(t *testing.T) {
	sent := renderFrames(t,
		`{"type":"TEXT_MESSAGE_START","role":"assistant"}`,
		`{"type":"TEXT_MESSAGE_CONTENT","delta":"Working."}`,
		`{"type":"TOOL_CALL_START","toolCallName":"Bash"}`,
		`{"type":"TOOL_CALL_ARGS","delta":"{\"command\":\"ls -la\"}"}`,
		`{"type":"TOOL_CALL_END"}`,
		`{"type":"TOOL_CALL_RESULT","content":"{\"success\":false,\"error\":\"denied\"}"}`,
		`{"type":"TEXT_MESSAGE_CONTENT","delta":" Done."}`,
		`{"type":"TEXT_MESSAGE_END"}`,
	)
	want := []string{
		"> Bash: `{\"command\":\"ls -la\"}`",
		"> Tool failed - retrying may follow:\n> \"\"\n> denied\n> \"\"",
		"Working. Done.",
	}
	if len(sent) != len(want) {
		t.Fatalf("expected one message per render, got %v", sent)
	}
	for i, tt := range want {
		if sent[i].Content != tt {
			t.Errorf("sent[%d] = %q, want %q", i, sent[i].Content, tt)
		}
	}
}

// RenderRunError closes the turn the worker failed: anything still open,
// then the error the way the subprocess path forwarded agent errors.
func TestThreadChat_RenderRunError(t *testing.T) {
	sent := renderFrames(t,
		`{"type":"TEXT_MESSAGE_START","role":"assistant"}`,
		`{"type":"TEXT_MESSAGE_CONTENT","delta":"half answered"}`,
		`{"type":"RUN_ERROR","message":"context length exceeded"}`,
	)
	if len(sent) != 1 {
		t.Fatalf("expected exactly one message, got %v", sent)
	}
	if sent[0].Content != "half answered\n\nError: context length exceeded" {
		t.Errorf("got %q", sent[0].Content)
	}
}

func TestToolLine(t *testing.T) {
	if got := toolLine("Bash", ""); got != "Bash" {
		t.Errorf("expected the call alone without args, got %q", got)
	}
	if got := toolLine("Bash", `{"command":"ls"}`); got != "Bash: `{\"command\":\"ls\"}`" {
		t.Errorf("expected a compact single line, got %q", got)
	}

	got := toolLine("Read", strings.Repeat("x", maxToolResultLen+1))
	if !strings.HasSuffix(got, "…`") {
		t.Errorf("expected the long call capped with the ellipsis, got %d runes", len([]rune(got)))
	}
}

func TestQuotedToolResult(t *testing.T) {
	if got := quotedToolResult(`{"success":true}`); got != "> \"\"\n> {\"success\":true}\n> \"\"" {
		t.Errorf("expected the result as its own quoted block, got %q", got)
	}

	if got := quotedToolResult(`{"success":false,"error":"denied"}`); !strings.Contains(got, "denied") || !strings.HasPrefix(got, "> Tool failed - retrying may follow:\n") || !strings.HasSuffix(got, "\n> \"\"") {
		t.Errorf("expected the failure detail quoted for a retry, got %q", got)
	}

	if got := quotedToolResult(""); got != "" {
		t.Errorf("expected an empty result to stay off the chat, got %q", got)
	}

	got := quotedToolResult(`{"success":true,"result":"` + strings.Repeat("a", maxToolResultLen+1) + `"}`)
	if strings.Contains(got, strings.Repeat("a", maxToolResultLen+1)) || !strings.Contains(got, strings.Repeat("a", maxToolResultLen)) || !strings.Contains(got, "…") {
		t.Errorf("expected the long result capped at %d runes with the ellipsis, got %q", maxToolResultLen, got)
	}
}

// QuoteBlock prefixes every line so tool traffic arrives collapsed as a
// blockquote.
func TestQuoteBlock(t *testing.T) {
	if got := quoteBlock("one\ntwo"); got != "> one\n> two" {
		t.Errorf("got %q", got)
	}
}