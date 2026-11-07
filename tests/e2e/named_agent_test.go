//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	tokenless "github.com/inference-gateway/tokenless"
	gateway "github.com/inference-gateway/tokenless/gateway"
)

const (
	agentModel = "anthropic/claude-sonnet-4-5"
	agentBody  = "You are the e2e reviewer agent."
)

// writeUserAgent puts a Markdown agent into home's ~/.infer/agents.
func writeUserAgent(t *testing.T, home, name, frontmatter, body string) {
	t.Helper()
	dir := filepath.Join(home, ".infer", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	content := "---\nname: " + name + "\ndescription: An e2e agent\n" + frontmatter + "---\n\n" + body + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o600))
}

// replyText joins the assistant text a run streamed.
func replyText(events []map[string]any) string {
	var text strings.Builder
	for _, e := range events {
		if e["type"] == "TEXT_MESSAGE_CONTENT" {
			text.WriteString(e["delta"].(string))
		}
	}
	return text.String()
}

// requestsFor returns the completions the mock answered from the named scenario.
func requestsFor(m *tokenless.Mock, scenario string) []gateway.Recorded {
	var out []gateway.Recorded
	for _, r := range m.Requests() {
		if r.Scenario == scenario {
			out = append(out, r)
		}
	}
	return out
}

// requestBody is the part of a completions or messages request the agent
// shapes: the system prompt (a top-level field or a system message) and tools.
type requestBody struct {
	System   json.RawMessage `json:"system"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	Tools []struct {
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

func decodeRequest(t *testing.T, r gateway.Recorded) requestBody {
	t.Helper()
	var body requestBody
	require.NoError(t, json.Unmarshal(r.RawBody, &body))
	return body
}

// systemPrompt is the request's system prompt as raw JSON text.
func systemPrompt(t *testing.T, r gateway.Recorded) string {
	t.Helper()
	body := decodeRequest(t, r)
	prompt := string(body.System)
	for _, msg := range body.Messages {
		if msg.Role == string(gateway.System) {
			prompt += string(msg.Content)
		}
	}
	return prompt
}

// advertisedTools are the tool names the request offered the model.
func advertisedTools(t *testing.T, r gateway.Recorded) []string {
	t.Helper()
	tools := decodeRequest(t, r).Tools
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name+tool.Function.Name)
	}
	return names
}

func TestDaemonRunsAThreadAsItsNamedAgent(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	writeUserAgent(t, home, "e2e-reviewer", "model: "+agentModel+"\ntools:\n  - Read\n", agentBody)
	m := startMock(t)
	d := startDaemon(t, home, "INFER_GATEWAY_MOCK=false", "INFER_GATEWAY_RUN=false", "INFER_GATEWAY_URL="+m.URL)
	c := dialDaemon(t, d, "desktop")
	c.send(map[string]any{"type": "new_session", "project_dir": project, "mode": "auto", "model": testModel, "agent": "e2e-reviewer"})
	c.next("MESSAGES_SNAPSHOT")

	c.say("which model are you running on")
	require.Contains(t, replyText(c.run()), "Answered as the named agent.")
	c.send(map[string]any{"type": "select_model", "project_dir": project, "model": testModel})
	require.Equal(t, agentModel, c.next("models")["current"], "select_model must leave a pinned agent model alone")
	c.say("which model are you running on")
	c.run()

	asked := requestsFor(m, "named-agent-own-model")
	require.Len(t, asked, 2)
	for _, r := range asked {
		require.Equal(t, "anthropic", r.Provider, "the front-matter model's provider, not the thread's")
		require.Equal(t, "claude-sonnet-4-5", r.Model, "the front-matter model, before and after select_model")
		require.Contains(t, systemPrompt(t, r), agentBody, "the agent's body is the system prompt")
		require.Equal(t, []string{"Read"}, advertisedTools(t, r), "only the agent's tools are offered")
	}

	c.say("write agent-blocked.txt")
	events := c.run()
	require.Equal(t, "RUN_FINISHED", events[len(events)-1]["type"], "events: %v", events)
	require.Len(t, requestsFor(m, "named-agent-write-outside-its-tools"), 2, "the Write call must reach the worker")
	require.NoFileExists(t, filepath.Join(project, "agent-blocked.txt"), "a tool outside the agent's tools must not run")
}

func TestDaemonKeepsTheThreadModelForAnInheritAgent(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	writeUserAgent(t, home, "e2e-inherit", "model: inherit\n", agentBody)
	m := startMock(t)
	d := startDaemon(t, home, "INFER_GATEWAY_MOCK=false", "INFER_GATEWAY_RUN=false", "INFER_GATEWAY_URL="+m.URL)
	c := dialDaemon(t, d, "desktop")
	c.send(map[string]any{"type": "new_session", "project_dir": project, "mode": "auto", "model": testModel, "agent": "e2e-inherit"})
	c.next("MESSAGES_SNAPSHOT")

	c.send(map[string]any{"type": "select_model", "project_dir": project, "model": agentModel})
	require.Equal(t, agentModel, c.next("models")["current"], "an inherit agent's thread still switches models")
	c.say("which model are you running on")
	c.run()

	asked := requestsFor(m, "named-agent-own-model")
	require.Len(t, asked, 1)
	require.Equal(t, "claude-sonnet-4-5", asked[0].Model)
	require.Contains(t, systemPrompt(t, asked[0]), agentBody)
}

func TestDaemonRefusesAThreadForAnUnknownAgent(t *testing.T) {
	d := startDaemon(t, t.TempDir())
	c := dialDaemon(t, d, "desktop")
	c.send(map[string]any{"type": "new_session", "project_dir": t.TempDir(), "model": testModel, "agent": "nope"})
	require.Contains(t, c.next("RUN_ERROR")["message"], `unknown agent "nope"`)
}
