package ui

import (
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func sampleQuestions() []agentdomain.UserQuestion {
	return []agentdomain.UserQuestion{
		{Header: "Format", Question: "fmt?", MultiSelect: false, Options: []agentdomain.UserQuestionOption{{Label: "JSON"}, {Label: "YAML"}}},
		{Header: "Scope", Question: "scope?", MultiSelect: true, Options: []agentdomain.UserQuestionOption{{Label: "A"}, {Label: "B"}, {Label: "C"}}},
	}
}

func TestUserQuestionUIState_SetupAndGet(t *testing.T) {
	s := NewApplicationState()
	s.SetupUserQuestionUIState(sampleQuestions(), make(chan []agentdomain.UserQuestionAnswer, 1))

	st := s.GetUserQuestionUIState()
	if st == nil || len(st.Questions) != 2 {
		t.Fatalf("expected state with 2 questions, got %+v", st)
	}
}

func TestUserQuestionUIState_ClearClosesChannel(t *testing.T) {
	s := NewApplicationState()
	ch := make(chan []agentdomain.UserQuestionAnswer, 1)
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
