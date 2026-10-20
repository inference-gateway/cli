package loop

import (
	"encoding/json"
	"slices"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	tools "github.com/inference-gateway/cli/internal/tools"
)

const (
	restoredSkillMaxChars    = 20_000
	restoredSkillsTotalChars = 100_000
	restoredSkillTruncated   = "\n[truncated]"
)

// skillLoads returns the names of the completed Skill calls in messages, most
// recent first and deduplicated. A call counts only once its tool result is
// present, so the call being executed right now never counts as already loaded.
func skillLoads(messages []sdk.Message) []string {
	answered := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Role == sdk.Tool && msg.ToolCallID != nil {
			answered[*msg.ToolCallID] = struct{}{}
		}
	}
	var names []string
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != sdk.Assistant || msg.ToolCalls == nil {
			continue
		}
		for _, tc := range *msg.ToolCalls {
			if _, done := answered[tc.ID]; !done || tc.Function.Name != tools.ToolSkill {
				continue
			}
			if name := skillCallName(tc.Function.Arguments); name != "" && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	return names
}

func skillCallName(arguments string) string {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return ""
	}
	return bareSkillName(args.Name)
}

func bareSkillName(name string) string {
	if i := strings.LastIndexByte(name, ':'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// loadedSkills returns the skills already loaded in messages, most recent
// first. "Loaded" is derived from the conversation itself, so it is per
// session with no extra state.
func loadedSkills(skills agentdomain.SkillsService, messages []sdk.Message) []agentdomain.Skill {
	if skills == nil {
		return nil
	}
	var loaded []agentdomain.Skill
	for _, name := range skillLoads(messages) {
		if sk, ok := skills.Get(name); ok && sk.Path != "" {
			loaded = append(loaded, sk)
		}
	}
	return loaded
}

// SkillRestorer returns the compaction restore hook: for every skill loaded in
// the summarized range it re-emits a Skill tool call and its result so the
// skill's instructions survive the summary, newest first under a total budget.
func SkillRestorer(skills agentdomain.SkillsService) func(summarized []sdk.Message) []sdk.Message {
	return func(summarized []sdk.Message) []sdk.Message {
		var restored []sdk.Message
		budget := restoredSkillsTotalChars
		for _, sk := range loadedSkills(skills, summarized) {
			body, err := tools.LoadSkillBody(sk.Path)
			if err != nil {
				logger.Debug("skipping skill restore, file unreadable", "skill", sk.Name, "path", sk.Path, "error", err)
				continue
			}
			content := truncateSkillBody(body, min(restoredSkillMaxChars, budget))
			if content == "" {
				break
			}
			budget -= len(content)
			restored = append(restored, restoredSkillPair(sk, content)...)
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

func restoredSkillPair(sk agentdomain.Skill, content string) []sdk.Message {
	id := "restored-skill-" + sk.Name
	args, _ := json.Marshal(map[string]string{"name": sk.DisplayName()})
	return []sdk.Message{
		{
			Role:    sdk.Assistant,
			Content: sdk.NewMessageContent(""),
			ToolCalls: &[]sdk.ChatCompletionMessageToolCall{{
				ID:       id,
				Type:     sdk.Function,
				Function: sdk.ChatCompletionMessageToolCallFunction{Name: tools.ToolSkill, Arguments: string(args)},
			}},
		},
		{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent(tools.FormatSkillForLLM(sk.DisplayName(), sk.Path, content)),
			ToolCallID: &id,
		},
	}
}

// alreadyLoadedSkillResult is returned instead of the body when the model
// loads a skill whose SKILL.md is already in the conversation.
func alreadyLoadedSkillResult(sk agentdomain.Skill) string {
	return "Skill " + sk.DisplayName() + " is already loaded in this conversation. Follow its instructions above - " +
		"they were not repeated here to save context."
}
