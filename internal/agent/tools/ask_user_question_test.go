package tools

import (
	"context"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func newAskUserQuestionToolForTest() *AskUserQuestionTool {
	cfg := &config.Config{
		Prompts: *config.DefaultPromptsConfig(),
	}
	return NewAskUserQuestionTool(cfg)
}

// option builds a raw option map as it arrives from JSON unmarshalling.
func option(label, desc string) map[string]any {
	return map[string]any{"label": label, "description": desc}
}

// question builds a raw question map as it arrives from JSON unmarshalling.
func question(header, text string, multi bool, opts ...map[string]any) map[string]any {
	rawOpts := make([]any, 0, len(opts))
	for _, o := range opts {
		rawOpts = append(rawOpts, o)
	}
	return map[string]any{
		"header":      header,
		"question":    text,
		"multiSelect": multi,
		"options":     rawOpts,
	}
}

func validQuestionArgs() map[string]any {
	return map[string]any{
		"questions": []any{
			question("Format", "Which output format?", false,
				option("JSON", "machine readable"),
				option("YAML", "human readable"),
			),
		},
	}
}

// stubBroker is an in-memory UserQuestionBroker for Execute tests.
type stubBroker struct {
	answers   []domain.UserQuestionAnswer
	ok        bool
	err       error
	received  []domain.UserQuestion
	callCount int
}

func (s *stubBroker) AskUserQuestions(_ context.Context, questions []domain.UserQuestion) ([]domain.UserQuestionAnswer, bool, error) {
	s.callCount++
	s.received = questions
	return s.answers, s.ok, s.err
}

func TestAskUserQuestionTool_Definition(t *testing.T) {
	tool := newAskUserQuestionToolForTest()
	def := tool.Definition()

	if def.Function.Name != "AskUserQuestion" {
		t.Fatalf("expected name AskUserQuestion, got %q", def.Function.Name)
	}
	if def.Function.Description == nil || *def.Function.Description == "" {
		t.Fatal("expected a non-empty description")
	}

	params := *def.Function.Parameters
	required, ok := params["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "questions" {
		t.Fatalf("expected required=[questions], got %v", params["required"])
	}

	props, _ := params["properties"].(map[string]any)
	questions, _ := props["questions"].(map[string]any)
	if questions["minItems"] != minQuestions || questions["maxItems"] != maxQuestions {
		t.Fatalf("expected questions minItems=%d maxItems=%d, got %v/%v", minQuestions, maxQuestions, questions["minItems"], questions["maxItems"])
	}
}

func TestAskUserQuestionTool_Validate(t *testing.T) {
	tool := newAskUserQuestionToolForTest()

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{
			name:    "valid single-select",
			args:    validQuestionArgs(),
			wantErr: false,
		},
		{
			name: "valid four questions multi-select",
			args: map[string]any{"questions": []any{
				question("A", "qa", true, option("1", "d"), option("2", "d"), option("3", "d"), option("4", "d")),
				question("B", "qb", false, option("1", "d"), option("2", "d")),
				question("C", "qc", false, option("1", "d"), option("2", "d")),
				question("D", "qd", true, option("1", "d"), option("2", "d")),
			}},
			wantErr: false,
		},
		{
			name:    "no questions key",
			args:    map[string]any{},
			wantErr: true,
		},
		{
			name:    "empty questions",
			args:    map[string]any{"questions": []any{}},
			wantErr: true,
		},
		{
			name: "too many questions",
			args: map[string]any{"questions": []any{
				question("A", "qa", false, option("1", "d"), option("2", "d")),
				question("B", "qb", false, option("1", "d"), option("2", "d")),
				question("C", "qc", false, option("1", "d"), option("2", "d")),
				question("D", "qd", false, option("1", "d"), option("2", "d")),
				question("E", "qe", false, option("1", "d"), option("2", "d")),
			}},
			wantErr: true,
		},
		{
			name:    "header too long",
			args:    map[string]any{"questions": []any{question("ThisHeaderIsWayTooLong", "q", false, option("1", "d"), option("2", "d"))}},
			wantErr: true,
		},
		{
			name:    "empty header",
			args:    map[string]any{"questions": []any{question("", "q", false, option("1", "d"), option("2", "d"))}},
			wantErr: true,
		},
		{
			name:    "empty question text",
			args:    map[string]any{"questions": []any{question("H", "", false, option("1", "d"), option("2", "d"))}},
			wantErr: true,
		},
		{
			name:    "too few options",
			args:    map[string]any{"questions": []any{question("H", "q", false, option("1", "d"))}},
			wantErr: true,
		},
		{
			name:    "too many options",
			args:    map[string]any{"questions": []any{question("H", "q", false, option("1", "d"), option("2", "d"), option("3", "d"), option("4", "d"), option("5", "d"))}},
			wantErr: true,
		},
		{
			name:    "empty option label",
			args:    map[string]any{"questions": []any{question("H", "q", false, option("", "d"), option("2", "d"))}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if tt.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestAskUserQuestionTool_Execute_HeadlessDegrade(t *testing.T) {
	tool := newAskUserQuestionToolForTest()

	// No broker in context -> degrade gracefully without blocking.
	result, err := tool.Execute(context.Background(), validQuestionArgs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error %q", result.Error)
	}
	data, _ := result.Data.(map[string]any)
	if data["available"] != false {
		t.Fatalf("expected available=false, got %v", data["available"])
	}
	if msg, _ := data["message"].(string); !strings.Contains(msg, "No interactive user") {
		t.Fatalf("expected degrade message, got %q", msg)
	}
}

func TestAskUserQuestionTool_Execute_WithBroker(t *testing.T) {
	tool := newAskUserQuestionToolForTest()

	broker := &stubBroker{
		ok: true,
		answers: []domain.UserQuestionAnswer{
			{Header: "Format", Question: "Which output format?", SelectedLabels: []string{"JSON"}},
		},
	}
	ctx := domain.WithUserQuestionBroker(context.Background(), broker)

	result, err := tool.Execute(ctx, validQuestionArgs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error %q", result.Error)
	}
	if broker.callCount != 1 {
		t.Fatalf("expected broker to be called once, got %d", broker.callCount)
	}
	if len(broker.received) != 1 || broker.received[0].Header != "Format" {
		t.Fatalf("broker did not receive parsed questions: %+v", broker.received)
	}
	data, _ := result.Data.(map[string]any)
	answers, _ := data["answers"].([]domain.UserQuestionAnswer)
	if len(answers) != 1 || answers[0].SelectedLabels[0] != "JSON" {
		t.Fatalf("unexpected answers in result: %+v", data["answers"])
	}
	if msg, _ := data["message"].(string); !strings.Contains(msg, "JSON") {
		t.Fatalf("expected formatted message to mention JSON, got %q", msg)
	}
}

func TestAskUserQuestionTool_Execute_Cancelled(t *testing.T) {
	tool := newAskUserQuestionToolForTest()

	broker := &stubBroker{ok: false}
	ctx := domain.WithUserQuestionBroker(context.Background(), broker)

	result, err := tool.Execute(ctx, validQuestionArgs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error %q", result.Error)
	}
	data, _ := result.Data.(map[string]any)
	if data["cancelled"] != true {
		t.Fatalf("expected cancelled=true, got %v", data["cancelled"])
	}
}

func TestAskUserQuestionTool_Execute_InvalidArgs(t *testing.T) {
	tool := newAskUserQuestionToolForTest()

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure for invalid args")
	}
	if result.Error == "" {
		t.Fatal("expected an error message")
	}
}

// TestAskUserQuestionTool_ResultReachesLLMContext verifies the precise link that
// puts the answers into the conversation the model sees next turn: the agent's
// executeToolInternal feeds the tool result through FormatToolResultForLLM, which
// dispatches to this tool's FormatResult(FormatterLLM). That string becomes the
// tool-result message content.
func TestAskUserQuestionTool_ResultReachesLLMContext(t *testing.T) {
	tool := newAskUserQuestionToolForTest()
	broker := &stubBroker{ok: true, answers: []domain.UserQuestionAnswer{
		{Header: "Backend", Question: "Which storage backend?", SelectedLabels: []string{"sqlite"}},
		{Header: "Scope", Question: "Which areas?", SelectedLabels: []string{"agent", "ui"}, OtherText: "also config"},
	}}
	ctx := domain.WithUserQuestionBroker(context.Background(), broker)

	result, err := tool.Execute(ctx, validQuestionArgs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	llm := tool.FormatResult(result, domain.FormatterLLM)
	for _, want := range []string{"Backend", "sqlite", "Scope", "agent, ui", `Other: "also config"`} {
		if !strings.Contains(llm, want) {
			t.Errorf("LLM-facing tool result is missing %q; got:\n%s", want, llm)
		}
	}
}

func TestFormatAnswersForLLM(t *testing.T) {
	answers := []domain.UserQuestionAnswer{
		{Header: "Format", Question: "Which output format?", SelectedLabels: []string{"JSON"}},
		{Header: "Scope", Question: "Which files?", SelectedLabels: []string{"internal/agent", "internal/ui"}, OtherText: "also config/"},
		{Header: "Mode", Question: "Pick one", OtherText: "custom only"},
	}

	out := formatAnswersForLLM(answers)

	if !strings.Contains(out, "[Format] Which output format? -> JSON") {
		t.Errorf("missing single-label line: %q", out)
	}
	if !strings.Contains(out, "internal/agent, internal/ui") {
		t.Errorf("missing multi-label join: %q", out)
	}
	if !strings.Contains(out, `Other: "also config/"`) {
		t.Errorf("missing other text: %q", out)
	}
	if !strings.Contains(out, `[Mode] Pick one -> Other: "custom only"`) {
		t.Errorf("missing other-only line: %q", out)
	}
}
