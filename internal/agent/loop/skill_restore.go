package loop

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	tools "github.com/inference-gateway/cli/internal/tools"
)

const (
	restoredSkillMaxChars    = 20_000
	restoredSkillsTotalChars = 100_000
	restoredSkillTruncated   = "\n[truncated - Read the file with a line range for the rest]"
)

// readToolPaths returns the file paths of whole-file Read tool calls in
// messages, most recent first. A ranged read (offset or limit) is partial and
// never counts as having loaded the file.
func readToolPaths(messages []sdk.Message) []string {
	var paths []string
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != sdk.Assistant || msg.ToolCalls == nil {
			continue
		}
		for _, tc := range *msg.ToolCalls {
			if path, ok := wholeFileReadPath(tc); ok && !containsString(paths, path) {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func wholeFileReadPath(tc sdk.ChatCompletionMessageToolCall) (string, bool) {
	if tc.Function.Name != tools.ToolRead {
		return "", false
	}
	var args struct {
		FilePath string `json:"file_path"`
		Offset   *int   `json:"offset"`
		Limit    *int   `json:"limit"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return "", false
	}
	if args.FilePath == "" || args.Offset != nil || args.Limit != nil {
		return "", false
	}
	return args.FilePath, true
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// loadedSkills returns the skills whose SKILL.md was read in full somewhere in
// messages, most recently read first. "Loaded" is derived from the
// conversation itself, so it is per session with no extra state.
func loadedSkills(skills agentdomain.SkillsService, messages []sdk.Message) []agentdomain.Skill {
	if skills == nil {
		return nil
	}
	byPath := make(map[string]agentdomain.Skill)
	for _, sk := range skills.List() {
		if sk.Path != "" {
			byPath[sk.Path] = sk
		}
	}
	var loaded []agentdomain.Skill
	for _, path := range readToolPaths(messages) {
		if sk, ok := byPath[path]; ok {
			loaded = append(loaded, sk)
		}
	}
	return loaded
}

// SkillRestorer returns the compaction restore hook: for every skill loaded in
// the summarized range it re-emits the Read tool call and its result so the
// skill's instructions survive the summary, newest first under a total budget.
func SkillRestorer(skills agentdomain.SkillsService) func(summarized []sdk.Message) []sdk.Message {
	return func(summarized []sdk.Message) []sdk.Message {
		var restored []sdk.Message
		budget := restoredSkillsTotalChars
		for _, sk := range loadedSkills(skills, summarized) {
			body, err := os.ReadFile(sk.Path)
			if err != nil {
				logger.Debug("skipping skill restore, file unreadable", "skill", sk.Name, "path", sk.Path, "error", err)
				continue
			}
			content := truncateSkillBody(string(body), min(restoredSkillMaxChars, budget))
			if content == "" {
				break
			}
			budget -= len(content)
			restored = append(restored, restoredReadPair(sk, content, len(body))...)
		}
		return restored
	}
}

func truncateSkillBody(body string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	if len(body) <= maxChars {
		return body
	}
	return body[:maxChars] + restoredSkillTruncated
}

func restoredReadPair(sk agentdomain.Skill, content string, size int) []sdk.Message {
	id := "restored-skill-" + sk.Name
	args, _ := json.Marshal(map[string]string{"file_path": sk.Path})
	result := fmt.Sprintf("File: %s\nLines: %d\nSize: %d bytes\nContent:\n%s\n",
		sk.Path, strings.Count(content, "\n")+1, size, content)
	return []sdk.Message{
		{
			Role:    sdk.Assistant,
			Content: sdk.NewMessageContent(""),
			ToolCalls: &[]sdk.ChatCompletionMessageToolCall{{
				ID:       id,
				Type:     sdk.Function,
				Function: sdk.ChatCompletionMessageToolCallFunction{Name: tools.ToolRead, Arguments: string(args)},
			}},
		},
		{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent(result),
			ToolCallID: &id,
		},
	}
}

// alreadyLoadedSkillResult is the Read result returned instead of the body
// when the model re-reads a skill whose SKILL.md is already in the conversation.
func alreadyLoadedSkillResult(sk agentdomain.Skill) string {
	return fmt.Sprintf("Skill %q is already loaded in this conversation (its SKILL.md was read earlier). "+
		"Follow those instructions. Read the file with an explicit offset and limit if you need to see it again.", sk.DisplayName())
}
