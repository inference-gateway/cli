package domain

import (
	"context"
	"testing"
)

type fakeBroker struct{}

func (fakeBroker) AskUserQuestions(context.Context, []UserQuestion) ([]UserQuestionAnswer, bool, error) {
	return nil, false, nil
}

func TestWithUserQuestionBroker(t *testing.T) {
	ctx := context.Background()
	if GetUserQuestionBroker(ctx) != nil {
		t.Fatal("expected nil broker on empty context")
	}
	if HasUserQuestionBroker(ctx) {
		t.Fatal("expected HasUserQuestionBroker=false on empty context")
	}

	ctx = WithUserQuestionBroker(ctx, fakeBroker{})
	if GetUserQuestionBroker(ctx) == nil {
		t.Fatal("expected a broker after WithUserQuestionBroker")
	}
	if !HasUserQuestionBroker(ctx) {
		t.Fatal("expected HasUserQuestionBroker=true")
	}
}

func sampleQuestions() []UserQuestion {
	return []UserQuestion{
		{Header: "Format", Question: "fmt?", MultiSelect: false, Options: []UserQuestionOption{{Label: "JSON"}, {Label: "YAML"}}},
		{Header: "Scope", Question: "scope?", MultiSelect: true, Options: []UserQuestionOption{{Label: "A"}, {Label: "B"}, {Label: "C"}}},
	}
}

func TestUserQuestionUIState_SetupAndGet(t *testing.T) {
	s := NewApplicationState()
	s.SetupUserQuestionUIState(sampleQuestions(), make(chan []UserQuestionAnswer, 1))

	st := s.GetUserQuestionUIState()
	if st == nil || len(st.Questions) != 2 {
		t.Fatalf("expected state with 2 questions, got %+v", st)
	}
}

func TestUserQuestionUIState_ClearClosesChannel(t *testing.T) {
	s := NewApplicationState()
	ch := make(chan []UserQuestionAnswer, 1)
	s.SetupUserQuestionUIState(sampleQuestions(), ch)

	s.ClearUserQuestionUIState()
	if s.GetUserQuestionUIState() != nil {
		t.Fatal("expected nil state after clear")
	}
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("expected the response channel to be closed")
		}
	default:
		t.Fatal("expected a closed channel to be immediately readable")
	}

	s.ClearUserQuestionUIState()
}
