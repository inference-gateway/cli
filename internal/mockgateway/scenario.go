package mockgateway

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	sdk "github.com/inference-gateway/sdk"
	yaml "gopkg.in/yaml.v3"
)

//go:embed scenarios.yaml
var embeddedScenarios []byte

// injectableStatuses are the HTTP statuses allowed for error injection. 400
// is deliberately excluded: the CLI's default client.retry configuration
// treats it as retryable, which would turn an intended hard failure into a
// surprising retry storm for scenario authors.
var injectableStatuses = map[int]bool{408: true, 429: true, 500: true, 502: true, 503: true, 504: true}

// ScenarioFile is the root of a scenarios YAML document.
type ScenarioFile struct {
	// Fallback is rendered when no scenario matches the prompt or when a
	// matched scenario has no turn left for the current step.
	Fallback Turn `yaml:"fallback"`
	// Scenarios are evaluated in file order; the first regex match wins.
	Scenarios []Scenario `yaml:"scenarios"`
}

// Scenario is one scripted conversation, selected by regex.
type Scenario struct {
	// Name uniquely identifies the scenario in recordings and logs.
	Name string `yaml:"name"`
	// Match is a Go regular expression tested (unanchored) against the latest
	// user message of each request that is not an injected <system-reminder>.
	Match string `yaml:"match"`
	// Turns are the scripted assistant responses, indexed by the number of
	// assistant messages following the matched user message.
	Turns []Turn `yaml:"turns"`

	re *regexp.Regexp
}

// Turn is one scripted assistant response, rendered as SSE or as a sync JSON
// body depending on the request's stream flag.
type Turn struct {
	// Content is the assistant text, streamed in ChunkSize-rune fragments.
	Content string `yaml:"content"`
	// Reasoning is streamed as reasoning_content deltas before Content.
	Reasoning string `yaml:"reasoning"`
	// ToolCalls all land in this single assistant turn.
	ToolCalls []ToolCall `yaml:"tool_calls"`
	// Usage defaults to 10 prompt / 5 completion tokens when nil.
	Usage *Usage `yaml:"usage"`
	// ChunkSize is the fragment size in runes for streamed text (default 16).
	ChunkSize int `yaml:"chunk_size"`
	// DelayMs sleeps before each SSE frame (streaming) or once before the
	// body (sync), aborting early when the client disconnects.
	DelayMs int `yaml:"delay_ms"`
	// Error, when set, replaces the turn with an HTTP error for the first
	// Times matching requests (-1 means every request).
	Error *ErrorInject `yaml:"error"`
	// Malformed emits one non-JSON data: frame early in the stream.
	Malformed bool `yaml:"malformed"`
}

// ToolCall describes one function call the mock model requests.
type ToolCall struct {
	// Name is the tool name as registered in the CLI (e.g. Read, Grep, Bash).
	Name string `yaml:"name"`
	// Args is marshaled into the tool call's JSON arguments string.
	Args map[string]any `yaml:"args"`

	argsJSON string
}

// Usage overrides the token usage reported for a turn.
type Usage struct {
	PromptTokens     int64 `yaml:"prompt_tokens"`
	CompletionTokens int64 `yaml:"completion_tokens"`
}

// ErrorInject makes a turn answer with HTTP Status for the first Times
// requests that resolve to it; Times -1 fails forever.
type ErrorInject struct {
	Status int `yaml:"status"`
	Times  int `yaml:"times"`
}

// Default returns the embedded built-in scenario library.
func Default() *ScenarioFile {
	f, err := Load(embeddedScenarios)
	if err != nil {
		panic(fmt.Sprintf("mockgateway: embedded scenarios.yaml is invalid: %v", err))
	}
	return f
}

// Load parses and validates a scenarios YAML document. Unknown fields are
// rejected so typos in scenario files fail fast.
func Load(b []byte) (*ScenarioFile, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)

	var f ScenarioFile
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parsing scenarios: %w", err)
	}
	if err := f.validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

func (f *ScenarioFile) validate() error {
	if err := f.Fallback.validate("fallback"); err != nil {
		return err
	}

	seen := make(map[string]bool, len(f.Scenarios))
	for i := range f.Scenarios {
		if err := f.Scenarios[i].validate(seen); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scenario) validate(seen map[string]bool) error {
	if s.Name == "" {
		return fmt.Errorf("scenario with match %q: name is required", s.Match)
	}
	if seen[s.Name] {
		return fmt.Errorf("scenario %q: duplicate name", s.Name)
	}
	seen[s.Name] = true

	if s.Match == "" {
		return fmt.Errorf("scenario %q: match is required", s.Name)
	}
	re, err := regexp.Compile(s.Match)
	if err != nil {
		return fmt.Errorf("scenario %q: invalid match: %w", s.Name, err)
	}
	s.re = re

	if len(s.Turns) == 0 {
		return fmt.Errorf("scenario %q: at least one turn is required", s.Name)
	}
	for i := range s.Turns {
		if err := s.Turns[i].validate(fmt.Sprintf("scenario %q turn %d", s.Name, i)); err != nil {
			return err
		}
	}
	return nil
}

func (t *Turn) validate(where string) error {
	if t.ChunkSize < 0 || t.DelayMs < 0 {
		return fmt.Errorf("%s: chunk_size and delay_ms must be >= 0", where)
	}
	if t.Error != nil {
		if !injectableStatuses[t.Error.Status] {
			return fmt.Errorf("%s: error.status must be one of 408, 429, 500, 502, 503, 504", where)
		}
		if t.Error.Times == 0 || t.Error.Times < -1 {
			return fmt.Errorf("%s: error.times must be positive or -1", where)
		}
	}

	for i := range t.ToolCalls {
		tc := &t.ToolCalls[i]
		if tc.Name == "" {
			return fmt.Errorf("%s: tool_calls[%d].name is required", where, i)
		}
		if tc.Args == nil {
			tc.argsJSON = "{}"
			continue
		}
		args, err := json.Marshal(tc.Args)
		if err != nil {
			return fmt.Errorf("%s: tool_calls[%d].args: %w", where, i, err)
		}
		tc.argsJSON = string(args)
	}
	return nil
}

// resolve picks the scenario and turn for a request. It returns the matched
// scenario name ("" when only the fallback applies), the step derived from
// the assistant-message count after the anchor, and the turn to render.
func (f *ScenarioFile) resolve(req *sdk.CreateChatCompletionRequest) (string, int, Turn) {
	prompt, anchor := anchorUserMessage(req.Messages)

	step := 0
	for _, m := range req.Messages[anchor+1:] {
		if m.Role == sdk.Assistant {
			step++
		}
	}

	for i := range f.Scenarios {
		sc := &f.Scenarios[i]
		if !sc.re.MatchString(prompt) {
			continue
		}
		if step < len(sc.Turns) {
			return sc.Name, step, sc.Turns[step]
		}
		return sc.Name, step, f.Fallback
	}
	return "", step, f.Fallback
}

// anchorUserMessage returns the text and index of the latest user message that
// is not injected CLI content (<system-reminder>). Anchoring on the latest real
// prompt lets an interactive chat session re-route to a new scenario on every
// message, while the headless loop's automated-check reminder never re-routes
// a run mid-loop. Steps count the assistant messages after the anchor, so each
// new prompt restarts its scenario at turn 0.
func anchorUserMessage(messages []sdk.Message) (string, int) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != sdk.User {
			continue
		}
		text := textContent(messages[i].Content)
		if strings.Contains(text, "<system-reminder>") {
			continue
		}
		return text, i
	}
	return "", -1
}

// textContent extracts plain text from either content form: a bare string or
// a multimodal content-part array (text parts concatenated).
func textContent(c sdk.MessageContent) string {
	if s, err := c.AsMessageContent0(); err == nil {
		return s
	}

	parts, err := c.AsMessageContent1()
	if err != nil {
		return ""
	}

	var b strings.Builder
	for _, p := range parts {
		if tp, err := p.AsTextContentPart(); err == nil && tp.Type == sdk.TextContentPartTypeText {
			b.WriteString(tp.Text)
		}
	}
	return b.String()
}
