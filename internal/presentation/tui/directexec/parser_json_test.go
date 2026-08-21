package directexec

import "testing"

func TestService_ParseToolCall_JSONObject(t *testing.T) {
	svc := &Service{}
	input := `AskUserQuestion({"questions":[{"header":"Backend","question":"Which storage backend?","multiSelect":false,"options":[{"label":"sqlite","description":"Embedded"},{"label":"postgres","description":"Networked"}]}]})`

	name, args, err := svc.ParseToolCall(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "AskUserQuestion" {
		t.Fatalf("expected AskUserQuestion, got %q", name)
	}
	questions, ok := args["questions"].([]any)
	if !ok || len(questions) != 1 {
		t.Fatalf("expected questions to parse as a 1-element array, got %#v", args["questions"])
	}
	q0, _ := questions[0].(map[string]any)
	if q0["header"] != "Backend" {
		t.Fatalf("expected header Backend, got %v", q0["header"])
	}
}
