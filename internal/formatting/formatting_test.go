package formatting

import (
	"strings"
	"testing"
)

func TestFormatResponsiveMessage_NoTrailingSpaces(t *testing.T) {
	tests := []struct {
		name    string
		content string
		width   int
	}{
		{
			name:    "Long line that needs wrapping",
			content: "This is a very long line that will definitely need to be wrapped because it exceeds the specified width limit",
			width:   30,
		},
		{
			name:    "Multiple lines with wrapping",
			content: "First line that is quite long and needs wrapping\nSecond line also long\nThird",
			width:   20,
		},
		{
			name:    "Code block with long lines",
			content: "function calculateTotal(items, taxRate, discountPercentage) { return items.reduce((sum, item) => sum + item.price, 0) * (1 + taxRate) * (1 - discountPercentage); }",
			width:   40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatResponsiveMessage(tt.content, tt.width)
			lines := strings.Split(result, "\n")

			for i, line := range lines {
				if strings.HasSuffix(line, " ") {
					t.Errorf("Line %d has trailing spaces: %q", i+1, line)
				}
			}
		})
	}
}

func TestFormatResponsiveMessage_PreservesContent(t *testing.T) {
	content := "Hello world\nThis is a test\nWith multiple lines"
	width := 100

	result := FormatResponsiveMessage(content, width)

	if result != content {
		t.Errorf("Content was modified when it shouldn't have been wrapped\nExpected: %q\nGot: %q", content, result)
	}
}

func TestFormatResponsiveMessage_HandlesEmptyContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		width   int
		want    string
	}{
		{
			name:    "Empty string",
			content: "",
			width:   50,
			want:    "",
		},
		{
			name:    "Zero width",
			content: "Test content",
			width:   0,
			want:    "Test content",
		},
		{
			name:    "Negative width",
			content: "Test content",
			width:   -1,
			want:    "Test content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatResponsiveMessage(tt.content, tt.width)
			if result != tt.want {
				t.Errorf("FormatResponsiveMessage() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		name string
		cost float64
		want string
	}{
		{
			name: "Zero cost",
			cost: 0.0,
			want: "-",
		},
		{
			name: "Very small cost (4 decimals)",
			cost: 0.0023,
			want: "$0.0023",
		},
		{
			name: "Small cost under $0.01 (4 decimals)",
			cost: 0.0099,
			want: "$0.0099",
		},
		{
			name: "Cost exactly $0.01 (3 decimals)",
			cost: 0.01,
			want: "$0.010",
		},
		{
			name: "Cost between $0.01 and $1 (3 decimals)",
			cost: 0.142,
			want: "$0.142",
		},
		{
			name: "Cost just under $1 (3 decimals)",
			cost: 0.999,
			want: "$0.999",
		},
		{
			name: "Cost exactly $1 (2 decimals)",
			cost: 1.0,
			want: "$1.00",
		},
		{
			name: "Cost over $1 (2 decimals)",
			cost: 5.47,
			want: "$5.47",
		},
		{
			name: "Large cost (2 decimals)",
			cost: 123.45,
			want: "$123.45",
		},
		{
			name: "Cost with many decimals gets rounded",
			cost: 1.23456789,
			want: "$1.23",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatCost(tt.cost)
			if result != tt.want {
				t.Errorf("FormatCost(%v) = %q, want %q", tt.cost, result, tt.want)
			}
		})
	}
}
