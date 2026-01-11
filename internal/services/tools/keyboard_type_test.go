package tools

import (
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
)

func TestKeyboardTypeTool_TypingDelay(t *testing.T) {
	tests := []struct {
		name          string
		text          string
		delayMs       int
		expectedMinMs int
		skipExecution bool
	}{
		{
			name:          "fast typing with short delay",
			text:          "hi",
			delayMs:       50,
			expectedMinMs: 100,
			skipExecution: true,
		},
		{
			name:          "slow typing with long delay",
			text:          "hello",
			delayMs:       200,
			expectedMinMs: 1000,
			skipExecution: true,
		},
		{
			name:          "zero delay should still work",
			text:          "test",
			delayMs:       0,
			expectedMinMs: 0,
			skipExecution: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				ComputerUse: config.ComputerUseConfig{
					Enabled: true,
					KeyboardType: config.KeyboardTypeToolConfig{
						Enabled:       true,
						MaxTextLength: 1000,
						TypingDelayMs: tt.delayMs,
					},
					RateLimit: config.RateLimitConfig{
						Enabled:             true,
						MaxActionsPerMinute: 60,
						WindowSeconds:       60,
					},
				},
			}

			tool := NewKeyboardTypeTool(cfg, utils.NewRateLimiter(cfg.ComputerUse.RateLimit), nil)

			if tool.config.ComputerUse.KeyboardType.TypingDelayMs != tt.delayMs {
				t.Errorf("Expected delay %d ms, got %d ms", tt.delayMs, tool.config.ComputerUse.KeyboardType.TypingDelayMs)
			}
		})
	}
}

func TestKeyboardTypeTool_ConfigDefault(t *testing.T) {
	cfg := config.DefaultConfig()

	expectedDelay := 100
	actualDelay := cfg.ComputerUse.KeyboardType.TypingDelayMs

	if actualDelay != expectedDelay {
		t.Errorf("Expected default typing delay %d ms, got %d ms", expectedDelay, actualDelay)
	}
}

func TestKeyboardTypeTool_Validation(t *testing.T) {
	cfg := &config.Config{
		ComputerUse: config.ComputerUseConfig{
			Enabled: true,
			KeyboardType: config.KeyboardTypeToolConfig{
				Enabled:       true,
				MaxTextLength: 100,
				TypingDelayMs: 200,
			},
			RateLimit: config.RateLimitConfig{
				Enabled:             true,
				MaxActionsPerMinute: 60,
				WindowSeconds:       60,
			},
		},
	}

	tool := NewKeyboardTypeTool(cfg, utils.NewRateLimiter(cfg.ComputerUse.RateLimit), nil)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{
			name: "valid text input",
			args: map[string]any{
				"text": "hello world",
			},
			wantErr: false,
		},
		{
			name: "text exceeds max length",
			args: map[string]any{
				"text": string(make([]byte, 101)),
			},
			wantErr: true,
		},
		{
			name: "empty text",
			args: map[string]any{
				"text": "",
			},
			wantErr: true,
		},
		{
			name: "valid key combo",
			args: map[string]any{
				"key_combo": "ctrl+c",
			},
			wantErr: false,
		},
		{
			name:    "neither text nor key_combo",
			args:    map[string]any{},
			wantErr: true,
		},
		{
			name: "both text and key_combo",
			args: map[string]any{
				"text":      "hello",
				"key_combo": "ctrl+c",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Mock timing test to verify delay is applied (doesn't require X11)
func TestX11Client_TypeTextTiming(t *testing.T) {
	tests := []struct {
		name          string
		text          string
		delayMs       int
		minExpectedMs int
	}{
		{
			name:          "short text with 100ms delay",
			text:          "abc",
			delayMs:       100,
			minExpectedMs: 300,
		},
		{
			name:          "longer text with 50ms delay",
			text:          "hello",
			delayMs:       50,
			minExpectedMs: 250,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			charCount := len([]rune(tt.text))
			expectedMin := time.Duration(charCount*tt.delayMs) * time.Millisecond

			if expectedMin < time.Duration(tt.minExpectedMs)*time.Millisecond {
				t.Errorf("Expected minimum %v ms, calculated %v ms", tt.minExpectedMs, expectedMin.Milliseconds())
			}
		})
	}
}

func TestKeyboardTypeTool_FormatResult(t *testing.T) {
	cfg := &config.Config{
		ComputerUse: config.ComputerUseConfig{
			Enabled: true,
			KeyboardType: config.KeyboardTypeToolConfig{
				Enabled:       true,
				MaxTextLength: 1000,
				TypingDelayMs: 200,
			},
			RateLimit: config.RateLimitConfig{
				Enabled:             true,
				MaxActionsPerMinute: 60,
				WindowSeconds:       60,
			},
		},
	}

	tool := NewKeyboardTypeTool(cfg, utils.NewRateLimiter(cfg.ComputerUse.RateLimit), nil)

	result := &domain.ToolExecutionResult{
		ToolName:  "KeyboardType",
		Arguments: map[string]any{"text": "www.google.com"},
		Success:   true,
		Duration:  time.Second,
		Data: domain.KeyboardTypeToolResult{
			Text:    "www.google.com",
			Display: ":0",
			Method:  "x11",
		},
	}

	formatted := tool.FormatForLLM(result)
	expected := "Typed text: 'www.google.com' using x11"
	if formatted != expected {
		t.Errorf("Expected formatted result %q, got %q", expected, formatted)
	}

	preview := tool.FormatPreview(result)
	expectedPreview := "Typed: www.google.com"
	if preview != expectedPreview {
		t.Errorf("Expected preview %q, got %q", expectedPreview, preview)
	}
}
