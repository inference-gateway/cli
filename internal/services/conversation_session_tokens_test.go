package services

import (
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func TestSessionTokenTracking(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)

	stats := repo.GetSessionTokens()
	if stats.TotalInputTokens != 0 || stats.TotalOutputTokens != 0 || stats.TotalTokens != 0 || stats.RequestCount != 0 {
		t.Errorf("Expected zero stats initially, got %+v", stats)
	}

	err := repo.AddTokenUsage("test-model", 100, 50, 150, 0)
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

	err = repo.AddTokenUsage("test-model", 200, 75, 275, 0)
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

func TestAddCachedTokens(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)

	repo.AddCachedTokens(64)
	repo.AddCachedTokens(1920)

	if got := repo.GetSessionTokens().TotalCachedTokens; got != 1984 {
		t.Errorf("Expected 1984 cached tokens, got %d", got)
	}

	if err := repo.Clear(); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if got := repo.GetSessionTokens().TotalCachedTokens; got != 0 {
		t.Errorf("After clear, expected 0 cached tokens, got %d", got)
	}

	repo.SetSessionStats(domain.SessionTokenStats{TotalCachedTokens: 42}, domain.SessionCostStats{})
	if got := repo.GetSessionTokens().TotalCachedTokens; got != 42 {
		t.Errorf("SetSessionStats must restore cached tokens, got %d", got)
	}
}

// Regression: AddTokenUsage must forward the per-request cached-token count to
// CalculateCost so the session cost accumulator (which feeds the session_stats
// stream line and the /cost shortcut) discounts cache hits - it used to pass a
// hardcoded 0, inflating cost for cache-heavy runs.
func TestAddTokenUsageForwardsCachedTokensToPricing(t *testing.T) {
	pricing := &domainmocks.FakePricingService{}
	repo := NewInMemoryConversationRepository(nil, pricing)

	// 3000 prompt tokens, 2400 of them served from cache.
	if err := repo.AddTokenUsage("test-model", 3000, 100, 3100, 2400); err != nil {
		t.Fatalf("AddTokenUsage: %v", err)
	}

	if got := pricing.CalculateCostCallCount(); got != 1 {
		t.Fatalf("CalculateCost called %d times, want 1", got)
	}
	_, in, out, cached := pricing.CalculateCostArgsForCall(0)
	if in != 3000 || out != 100 || cached != 2400 {
		t.Errorf("CalculateCost args = in %d, out %d, cached %d; want 3000/100/2400 (cached must not be dropped to 0)", in, out, cached)
	}
}

func TestSessionTokensWithZeroValues(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)

	err := repo.AddTokenUsage("test-model", 0, 0, 0, 0)
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
