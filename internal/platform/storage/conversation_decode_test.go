package storage

import (
	"slices"
	"testing"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func TestTokenStats(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		output   int
		requests int
		want     convdomain.SessionTokenStats
	}{
		{"zero tally", 0, 0, 0, convdomain.SessionTokenStats{}},
		{"derives total from its halves", 120, 80, 3, convdomain.SessionTokenStats{
			TotalInputTokens: 120, TotalOutputTokens: 80, TotalTokens: 200, RequestCount: 3,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tokenStats(tt.input, tt.output, tt.requests); got != tt.want {
				t.Errorf("tokenStats(%d, %d, %d) = %+v, want %+v", tt.input, tt.output, tt.requests, got, tt.want)
			}
		})
	}
}

func TestDecodeCostStats(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want convdomain.SessionCostStats
	}{
		{"empty column", "", convdomain.SessionCostStats{}},
		{"empty object sentinel", "{}", convdomain.SessionCostStats{}},
		{"valid json", `{"TotalCost":1.25,"Currency":"USD"}`, convdomain.SessionCostStats{TotalCost: 1.25, Currency: "USD"}},
		{"corrupt json", `{"TotalCost":`, convdomain.SessionCostStats{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeCostStats(tt.raw)
			if got.TotalCost != tt.want.TotalCost || got.Currency != tt.want.Currency {
				t.Errorf("decodeCostStats(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestFirstModel(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty column", "", ""},
		{"empty array sentinel", "[]", ""},
		{"first entry of a valid array", `["anthropic/claude-4","openai/gpt-5"]`, "anthropic/claude-4"},
		{"corrupt json", `["anthropic`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstModel(tt.raw); got != tt.want {
				t.Errorf("firstModel(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestDecodeTags(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"empty column", "", nil},
		{"empty array sentinel", "[]", nil},
		{"valid array", `["code","review"]`, []string{"code", "review"}},
		{"corrupt json", "not-json", nil},
		{"partly valid array keeps what decoded in place", `["code",3]`, []string{"code", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decodeTags(tt.raw); !slices.Equal(got, tt.want) {
				t.Errorf("decodeTags(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
