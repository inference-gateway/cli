package config

import (
	"fmt"

	configutils "github.com/inference-gateway/cli/config/utils"
)

const (
	JudgeFileName    = "judge.yaml"
	DefaultJudgePath = ConfigDirName + "/" + JudgeFileName
)

// Judge approval outcomes for JudgeConfig.OnError: what happens when the judge
// call fails (timeout, gateway error, unparseable output).
const (
	JudgeOnErrorDeny  = "deny"
	JudgeOnErrorAllow = "allow"
)

// Default judge settings: the agent's own model decides, calls time out after
// 30s, and a failing judge denies - this is an approval gate and the existing
// no-approver behaviour is also block.
const (
	DefaultJudgeTimeoutSeconds = 30
	DefaultJudgeMaxTokens      = 256
)

// DefaultJudgePrompt is the user-overridable judge prompt. {intent} is the
// latest non-hidden user message and {action} the pending tool call.
const DefaultJudgePrompt = `You are the approver for an autonomous coding agent. Given the user's request, decide whether the pending tool call serves it and is safe to run. Respond with exactly {"decision": "approved" | "rejected", "reason": "..."}.

User request:
{intent}

Pending tool call:
{action}`

// JudgeConfig is the content of judge.yaml: how the LLM judge that decides
// tool approvals (approval_behaviour "judge" / agent mode auto-with-judge)
// is called. It is the decision-sibling of hooks.yaml / reminders.yaml: a
// separate file per concern. There is no enabled key - the judge runs only
// when the mode (or tools.safety.approval_behaviour) selects it.
type JudgeConfig struct {
	// Model is the "provider/model" id used for judge calls. Empty falls
	// back to agent.model (same precedent as conversation.title_generation.model).
	Model string `yaml:"model" mapstructure:"model"`
	// Timeout is the per-call timeout in seconds; 0 -> default.
	Timeout int `yaml:"timeout" mapstructure:"timeout"`
	// MaxTokens bounds each judge response; 0 -> default.
	MaxTokens int `yaml:"max_tokens" mapstructure:"max_tokens"`
	// OnError decides the verdict when the judge fails: deny (default) or allow.
	OnError string `yaml:"on_error" mapstructure:"on_error"`
	// Prompt is the user prompt template with {intent} and {action} placeholders.
	Prompt string `yaml:"prompt" mapstructure:"prompt"`
}

// DefaultJudgeConfig returns the in-code defaults used when no judge.yaml
// exists: empty model (falls back to agent.model), 30s timeout, 256 max
// tokens, deny-on-error, and the built-in prompt.
func DefaultJudgeConfig() *JudgeConfig {
	return &JudgeConfig{
		Timeout:   DefaultJudgeTimeoutSeconds,
		MaxTokens: DefaultJudgeMaxTokens,
		OnError:   JudgeOnErrorDeny,
		Prompt:    DefaultJudgePrompt,
	}
}

// LoadJudge reads judge.yaml from disk. When the file is missing it returns
// the in-code defaults so callers can treat absence as "use defaults".
func LoadJudge(path string) (*JudgeConfig, error) {
	return configutils.LoadYAML(path, "judge", DefaultJudgeConfig)
}

// ResolveModel returns the judge model, falling back to agentModel when the
// judge-specific model is unset.
func (j JudgeConfig) ResolveModel(agentModel string) string {
	if j.Model != "" {
		return j.Model
	}
	return agentModel
}

// effective applies per-field defaults: a non-positive timeout/max_tokens and
// an empty on_error or prompt fall back to the built-ins.
func (j JudgeConfig) effective() JudgeConfig {
	out := j
	out.Timeout = orPositive(j.Timeout, DefaultJudgeTimeoutSeconds)
	out.MaxTokens = orPositive(j.MaxTokens, DefaultJudgeMaxTokens)
	if out.OnError == "" {
		out.OnError = JudgeOnErrorDeny
	}
	if out.Prompt == "" {
		out.Prompt = DefaultJudgePrompt
	}
	return out
}

func orPositive(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

// Effective returns the judge config with defaults applied.
func (j JudgeConfig) Effective() JudgeConfig {
	return j.effective()
}

// Validate checks the judge config: on_error must be deny or allow.
func (j JudgeConfig) Validate() error {
	switch j.OnError {
	case "", JudgeOnErrorDeny, JudgeOnErrorAllow:
	default:
		return fmt.Errorf("judge.on_error %q: must be %q or %q", j.OnError, JudgeOnErrorDeny, JudgeOnErrorAllow)
	}
	return nil
}
