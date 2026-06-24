package config

// ModelMatcher defines a pattern match for context window estimation.
type ModelMatcher struct {
	Patterns      []string
	ContextWindow int
}

// DefaultContextWindow is the fallback context window size when no pattern matches.
const DefaultContextWindow = 8192

// ContextMatchers defines all model patterns in priority order.
// More specific patterns must come before less specific ones because matching
// uses strings.Contains (e.g. "gemma-3-1b" must precede "gemma-3", and
// "gemini-3" must precede "gemini-2"/"gemini").
//
// Coverage tracks the model IDs the gateway actually returns from /v1/models;
// model families that aren't served are intentionally omitted to keep the list
// small and the View output truthful.
var ContextMatchers = []ModelMatcher{
	{Patterns: []string{"fable"}, ContextWindow: 1000000},
	{Patterns: []string{"claude-opus-4-7", "claude-opus-4-8"}, ContextWindow: 1000000},
	{Patterns: []string{"claude-opus-4", "claude-sonnet-4", "claude-haiku-4"}, ContextWindow: 200000},
	{Patterns: []string{"claude"}, ContextWindow: 200000},
	{Patterns: []string{"gemini-3"}, ContextWindow: 1048576},
	{Patterns: []string{"gemini-2", "gemini-1.5"}, ContextWindow: 1000000},
	{Patterns: []string{"gemini-pro", "gemini-flash"}, ContextWindow: 1000000},
	{Patterns: []string{"gemini"}, ContextWindow: 32768},
	{Patterns: []string{"deep-research"}, ContextWindow: 1048576},
	{Patterns: []string{"gemma-3-1b", "gemma3:1b", "gemma-3n", "gemma3n"}, ContextWindow: 32768},
	{Patterns: []string{"gemma-3", "gemma3", "gemma-4", "gemma4"}, ContextWindow: 131072},
	{Patterns: []string{"gpt-oss"}, ContextWindow: 131072},
	{Patterns: []string{"deepseek-v4"}, ContextWindow: 1000000},
	{Patterns: []string{"deepseek-v3"}, ContextWindow: 131072},
	{Patterns: []string{"deepseek"}, ContextWindow: 131072},
	{Patterns: []string{"qwen3", "qwen-3"}, ContextWindow: 262144},
	{Patterns: []string{"qwen2.5", "qwen-2.5"}, ContextWindow: 131072},
	{Patterns: []string{"qwen"}, ContextWindow: 128000},
	{Patterns: []string{"ministral-3"}, ContextWindow: 256000},
	{Patterns: []string{"ministral"}, ContextWindow: 131072},
	{Patterns: []string{"mistral-large"}, ContextWindow: 131072},
	{Patterns: []string{"devstral"}, ContextWindow: 131072},
	{Patterns: []string{"mistral", "mixtral"}, ContextWindow: 32768},
	{Patterns: []string{"minimax-m2"}, ContextWindow: 204800},
	{Patterns: []string{"minimax-m3"}, ContextWindow: 1000000},
	{Patterns: []string{"glm-4", "glm-5"}, ContextWindow: 200000},
	{Patterns: []string{"nemotron-3"}, ContextWindow: 262144},
	{Patterns: []string{"kimi-k2", "kimi-latest"}, ContextWindow: 262144},
	{Patterns: []string{"moonshot-v1-128k"}, ContextWindow: 131072},
	{Patterns: []string{"moonshot-v1-32k"}, ContextWindow: 32768},
	{Patterns: []string{"moonshot-v1-8k", "moonshot-v1-auto"}, ContextWindow: 8192},
}
