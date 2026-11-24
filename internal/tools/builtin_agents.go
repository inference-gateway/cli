package tools

import (
	"embed"
	"io/fs"
	"path"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// builtinAgents holds the Markdown agents shipped with the binary. `infer init`
// seeds them into ~/.infer/agents for editing, and the loader falls back to
// them, so every name resolves even before init and a file always shadows one.
//
//go:embed builtin_agents
var builtinAgents embed.FS

const (
	builtinAgentsRoot = "builtin_agents"
	builtinSource     = "built-in"
)

// SeedBuiltinAgents writes each built-in agent into destDir, the user-scope
// agents directory, only when absent unless overwrite is set.
func SeedBuiltinAgents(destDir string, overwrite bool) error {
	return utils.SeedFS(builtinAgents, builtinAgentsRoot, destDir, overwrite)
}

// isBuiltinAgent reports whether name is one of the agents shipped with the
// binary, whose file init may have seeded and the user edited.
func isBuiltinAgent(name string) bool {
	_, err := fs.Stat(builtinAgents, path.Join(builtinAgentsRoot, name+".md"))
	return err == nil
}

// loadMarkdownAgentsWithBuiltins is loadMarkdownAgents plus the built-in agents
// whose names no file in dirs took.
func loadMarkdownAgentsWithBuiltins(dirs []string, knownTools map[string]bool) []markdownAgent {
	agents := loadMarkdownAgents(dirs, knownTools)
	taken := make(map[string]bool, len(agents))
	for _, agent := range agents {
		taken[agent.name] = true
	}
	for _, agent := range loadBuiltinAgents(knownTools) {
		if !taken[agent.name] {
			agents = append(agents, agent)
		}
	}
	return agents
}

// loadBuiltinAgents parses the embedded agents, labelled as built-in.
func loadBuiltinAgents(knownTools map[string]bool) []markdownAgent {
	entries, err := fs.ReadDir(builtinAgents, builtinAgentsRoot)
	if err != nil {
		logger.Warn("failed to read the built-in agents", "error", err)
		return nil
	}
	var agents []markdownAgent
	for _, entry := range entries {
		name := path.Join(builtinAgentsRoot, entry.Name())
		data, err := builtinAgents.ReadFile(name)
		if err != nil {
			logger.Warn("failed to read a built-in agent", "file", name, "error", err)
			continue
		}
		agent, reason := parseMarkdownAgent(data, name, knownTools)
		if reason != "" {
			logger.Warn("skipping invalid built-in agent", "file", name, "reason", reason)
			continue
		}
		agent.source = builtinSource
		agents = append(agents, *agent)
	}
	return agents
}

// MainAgentPrompt is the body of the main agent projectDir resolves - a
// project or user main.md, else the built-in one. It is the system prompt a
// chat starts with when prompts.agent.system_prompt does not override it.
func MainAgentPrompt(projectDir string) string {
	agent, err := findMarkdownAgent(loadMarkdownAgentsWithBuiltins(markdownAgentDirs(projectDir), nil), agentdomain.DefaultMainAgentName)
	if err != nil {
		logger.Warn("no main agent to take the system prompt from", "error", err)
		return ""
	}
	return agent.systemPrompt
}
