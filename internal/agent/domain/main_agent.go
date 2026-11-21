package domain

// DefaultMainAgentName is the built-in agent every chat runs as unless another
// is picked. Its body is the default system prompt.
const DefaultMainAgentName = "main"

// MainAgent is the agent a chat runs as, resolved from its Markdown file: the
// instructions it brings, the model it pins ("" keeps the chat's), and the tools
// and MCP servers it may call. Nil Tools or MCPServers allow every tool or
// server, an empty MCPServers none. The json names are a daemon frame's.
type MainAgent struct {
	Name            string   `json:"agent"`
	Instructions    string   `json:"system_prompt,omitempty"`
	Model           string   `json:"model,omitempty"`
	Tools           []string `json:"tools,omitempty"`
	DisallowedTools []string `json:"disallowed_tools,omitempty"`
	MCPServers      []string `json:"mcp_servers,omitzero"`
}

// MainAgentSelector switches the main agent a chat runs as between turns,
// keeping its conversation and its prompt cache.
type MainAgentSelector interface {
	SelectMainAgent(agent MainAgent) error
	MainAgent() MainAgent
}
