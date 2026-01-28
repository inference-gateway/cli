package config

// ModelMatcher defines a pattern match for context window estimation.
type ModelMatcher struct {
	Patterns      []string
	ContextWindow int
}

// DefaultContextWindow is the fallback context window size when no pattern matches.
const DefaultContextWindow = 8192

// ContextMatchers defines all model patterns in priority order.
// More specific patterns must come before less specific ones.
var ContextMatchers = []ModelMatcher{
	{Patterns: []string{"deepseek"}, ContextWindow: 128000},
	{Patterns: []string{"o1", "o3"}, ContextWindow: 200000},
	{Patterns: []string{"gpt-4o", "gpt-4-turbo"}, ContextWindow: 128000},
	{Patterns: []string{"gpt-4-32k"}, ContextWindow: 32768},
	{Patterns: []string{"gpt-4"}, ContextWindow: 8192},
	{Patterns: []string{"gpt-3.5"}, ContextWindow: 16384},
	{Patterns: []string{"claude-4", "claude-3.5", "claude-3"}, ContextWindow: 200000},
	{Patterns: []string{"claude-2"}, ContextWindow: 100000},
	{Patterns: []string{"claude"}, ContextWindow: 200000},
	{Patterns: []string{"gemini-2", "gemini-1.5"}, ContextWindow: 1000000},
	{Patterns: []string{"gemini"}, ContextWindow: 32768},
	{Patterns: []string{"mistral-large"}, ContextWindow: 128000},
	{Patterns: []string{"mistral", "mixtral"}, ContextWindow: 32768},
	{Patterns: []string{"llama-3.1", "llama-3.2", "llama-3.3"}, ContextWindow: 128000},
	{Patterns: []string{"llama-3"}, ContextWindow: 8192},
	{Patterns: []string{"llama"}, ContextWindow: 4096},
	{Patterns: []string{"qwen3", "qwen-3"}, ContextWindow: 262144},
	{Patterns: []string{"qwen2.5", "qwen-2.5"}, ContextWindow: 131072},
	{Patterns: []string{"qwen"}, ContextWindow: 128000},
	{Patterns: []string{"command-r"}, ContextWindow: 128000},
	{Patterns: []string{"kimi-k2", "kimi-latest"}, ContextWindow: 262144},
	{Patterns: []string{"moonshot-v1-128k"}, ContextWindow: 131072},
	{Patterns: []string{"moonshot-v1-32k"}, ContextWindow: 32768},
	{Patterns: []string{"moonshot-v1-8k", "moonshot-v1-auto"}, ContextWindow: 8192},
}
