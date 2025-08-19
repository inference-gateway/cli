package services

import (
	"testing"

	"github.com/inference-gateway/cli/internal/domain"
)

func TestSessionTokenTracking(t *testing.T) {
	repo := NewInMemoryConversationRepository()

	// Initially should have no tokens
	stats := repo.GetSessionTokens()
	if stats.TotalInputTokens != 0 || stats.TotalOutputTokens != 0 || stats.TotalTokens != 0 || stats.RequestCount != 0 {
		t.Errorf("Expected zero stats initially, got %+v", stats)
	}

	// Add first request tokens
	err := repo.AddTokenUsage(100, 50, 150)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	stats = repo.GetSessionTokens()
	expected := domain.SessionTokenStats{
		TotalInputTokens:  100,
		TotalOutputTokens: 50,
		TotalTokens:       150,
		RequestCount:      1,
	}
	if stats != expected {
		t.Errorf("Expected %+v, got %+v", expected, stats)
	}

	// Add second request tokens
	err = repo.AddTokenUsage(200, 75, 275)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	stats = repo.GetSessionTokens()
	expected = domain.SessionTokenStats{
		TotalInputTokens:  300, // 100 + 200
		TotalOutputTokens: 125, // 50 + 75
		TotalTokens:       425, // 150 + 275
		RequestCount:      2,
	}
	if stats != expected {
		t.Errorf("Expected %+v, got %+v", expected, stats)
	}

	// Test clear functionality
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
	repo := NewInMemoryConversationRepository()

	// Add request with zero tokens
	err := repo.AddTokenUsage(0, 0, 0)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	stats := repo.GetSessionTokens()
	expected := domain.SessionTokenStats{
		TotalInputTokens:  0,
		TotalOutputTokens: 0,
		TotalTokens:       0,
		RequestCount:      1, // Request count should still increment
	}
	if stats != expected {
		t.Errorf("Expected %+v, got %+v", expected, stats)
	}
}
