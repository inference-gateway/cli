package services

import (
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestSessionTokenTracking(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)

	stats := repo.GetSessionTokens()
	if stats.TotalInputTokens != 0 || stats.TotalOutputTokens != 0 || stats.TotalTokens != 0 || stats.RequestCount != 0 {
		t.Errorf("Expected zero stats initially, got %+v", stats)
	}

	err := repo.AddTokenUsage("test-model", 100, 50, 150)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	stats = repo.GetSessionTokens()
	expected := domain.SessionTokenStats{
		TotalInputTokens:  100,
		TotalOutputTokens: 50,
		TotalTokens:       150,
		RequestCount:      1,
		LastInputTokens:   100,
	}
	if stats != expected {
		t.Errorf("Expected %+v, got %+v", expected, stats)
	}

	err = repo.AddTokenUsage("test-model", 200, 75, 275)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	stats = repo.GetSessionTokens()
	expected = domain.SessionTokenStats{
		TotalInputTokens:  300,
		TotalOutputTokens: 125,
		TotalTokens:       425,
		RequestCount:      2,
		LastInputTokens:   200,
	}
	if stats != expected {
		t.Errorf("Expected %+v, got %+v", expected, stats)
	}

	err = repo.Clear()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	stats = repo.GetSessionTokens()
	expected = domain.SessionTokenStats{
		TotalInputTokens:  0,
		TotalOutputTokens: 0,
		TotalTokens:       0,
		RequestCount:      0,
	}
	if stats != expected {
		t.Errorf("After clear, expected %+v, got %+v", expected, stats)
	}
}

func TestSessionTokensWithZeroValues(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)

	err := repo.AddTokenUsage("test-model", 0, 0, 0)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	stats := repo.GetSessionTokens()
	expected := domain.SessionTokenStats{
		TotalInputTokens:  0,
		TotalOutputTokens: 0,
		TotalTokens:       0,
		RequestCount:      1,
	}
	if stats != expected {
		t.Errorf("Expected %+v, got %+v", expected, stats)
	}
}
