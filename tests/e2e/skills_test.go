//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	tokenless "github.com/inference-gateway/tokenless"
	mockgateway "github.com/inference-gateway/tokenless/gateway"
)

const demoSkillMarker = "DEMO_SKILL_MARKER"

// writeDemoSkill drops a project-scoped skill into dir. The body is padded so
// a single load crosses a 1% compaction threshold on a 128k window.
func writeDemoSkill(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, ".infer", "skills", "demo", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	body := "---\nname: demo\ndescription: Demo skill for e2e tests\n---\n# Demo\n" + demoSkillMarker + "\n" +
		strings.Repeat("Follow the demo conventions in every file you touch.\n", 200)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func TestSkillToolLoadsOnceThenReportsAlreadyLoaded(t *testing.T) {
	m := startMock(t)
	dir := t.TempDir()
	writeDemoSkill(t, dir)

	stdout, code := runAgent(t, m.URL, dir, "load the demo skill twice")
	require.Zero(t, code)

	toolResults := contentsByRole(jsonLines(t, stdout), "tool")
	require.Len(t, toolResults, 2)
	require.Contains(t, toolResults[0], demoSkillMarker, "the first Skill call returns the SKILL.md body")
	require.Contains(t, toolResults[1], "already loaded", "the second Skill call is short-circuited")
	require.NotContains(t, toolResults[1], demoSkillMarker, "the body is not sent twice")
}

func TestSkillSurvivesCompaction(t *testing.T) {
	m := startMock(t)
	dir := t.TempDir()
	writeDemoSkill(t, dir)

	env := inferEnv(m.URL)
	env["INFER_STORAGE_ENABLED"] = "true"
	env["INFER_COMPACT_ENABLED"] = "true"
	env["INFER_COMPACT_AUTO_AT"] = "1"
	res := tokenless.Orchestrator{Bin: binPath, Dir: dir, Env: env}.Run(t, "headless", "load the demo skill then keep going")
	require.Zero(t, res.ExitCode, res.Stderr)
	require.Contains(t, contentsByRole(jsonLines(t, res.Stdout), "assistant"), "Continuing after compaction.")

	last := m.Requests()[len(m.Requests())-1].Body
	require.True(t, hasUserMessageContaining(last, "--- Context Summary ---"), "the final turn runs on the compacted history")
	require.True(t, hasSkillCall(last, "demo"), "the loaded skill is re-emitted as a Skill call after the checkpoint")
	require.True(t, hasToolResultContaining(last, demoSkillMarker), "the restored Skill result carries the body")
}

func hasUserMessageContaining(body mockgateway.CreateChatCompletionRequest, want string) bool {
	for _, msg := range body.Messages {
		if msg.Role == mockgateway.User && strings.Contains(msg.Content.Text(), want) {
			return true
		}
	}
	return false
}

func hasToolResultContaining(body mockgateway.CreateChatCompletionRequest, want string) bool {
	for _, msg := range toolMessages(body) {
		if strings.Contains(msg.Content.Text(), want) {
			return true
		}
	}
	return false
}

func hasSkillCall(body mockgateway.CreateChatCompletionRequest, name string) bool {
	for _, msg := range body.Messages {
		if msg.ToolCalls == nil {
			continue
		}
		for _, tc := range *msg.ToolCalls {
			if tc.Function.Name == "Skill" && strings.Contains(tc.Function.Arguments, name) {
				return true
			}
		}
	}
	return false
}
