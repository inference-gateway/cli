package config

// PricingConfig holds configuration for model pricing and cost tracking.
type PricingConfig struct {
	Enabled      bool                     `yaml:"enabled" mapstructure:"enabled"`
	Currency     string                   `yaml:"currency" mapstructure:"currency"`
	CustomPrices map[string]CustomPricing `yaml:"custom_prices" mapstructure:"custom_prices"`
}

// CustomPricing allows users to override default pricing for specific models.
type CustomPricing struct {
	InputPricePerMToken  float64 `yaml:"input_price_per_mtoken" mapstructure:"input_price_per_mtoken"`
	OutputPricePerMToken float64 `yaml:"output_price_per_mtoken" mapstructure:"output_price_per_mtoken"`
}

// ModelPricing represents pricing information for a specific model.
// Prices are per million tokens to align with common pricing conventions.
type ModelPricing struct {
	Provider             string
	Model                string
	InputPricePerMToken  float64
	OutputPricePerMToken float64
	Currency             string
}

// GetDefaultPricingConfig returns the default pricing configuration.
func GetDefaultPricingConfig() PricingConfig {
	return PricingConfig{
		Enabled:      true,
		Currency:     "USD",
		CustomPrices: make(map[string]CustomPricing),
	}
}

// DefaultModelPricing contains hardcoded pricing for common models.
// Prices are based on publicly available pricing as of December 2024.
// Users can override these in their config files.
var DefaultModelPricing = map[string]ModelPricing{
	"anthropic/claude-opus-4-5-20251101": {
		Provider:             "anthropic",
		Model:                "claude-opus-4-5-20251101",
		InputPricePerMToken:  5.00,
		OutputPricePerMToken: 25.00,
		Currency:             "USD",
	},
	"anthropic/claude-haiku-4-5-20251001": {
		Provider:             "anthropic",
		Model:                "claude-haiku-4-5-20251001",
		InputPricePerMToken:  1.00,
		OutputPricePerMToken: 5.00,
		Currency:             "USD",
	},
	"anthropic/claude-sonnet-4-5-20250929": {
		Provider:             "anthropic",
		Model:                "claude-sonnet-4-5-20250929",
		InputPricePerMToken:  3.00,
		OutputPricePerMToken: 15.00,
		Currency:             "USD",
	},
	"anthropic/claude-opus-4-1-20250805": {
		Provider:             "anthropic",
		Model:                "claude-opus-4-1-20250805",
		InputPricePerMToken:  15.00,
		OutputPricePerMToken: 75.00,
		Currency:             "USD",
	},
	"anthropic/claude-opus-4-20250514": {
		Provider:             "anthropic",
		Model:                "claude-opus-4-20250514",
		InputPricePerMToken:  15.00,
		OutputPricePerMToken: 75.00,
		Currency:             "USD",
	},
	"anthropic/claude-sonnet-4-20250514": {
		Provider:             "anthropic",
		Model:                "claude-sonnet-4-20250514",
		InputPricePerMToken:  3.00,
		OutputPricePerMToken: 15.00,
		Currency:             "USD",
	},
	"anthropic/claude-3-7-sonnet-20250219": {
		Provider:             "anthropic",
		Model:                "claude-3-7-sonnet-20250219",
		InputPricePerMToken:  3.00,
		OutputPricePerMToken: 15.00,
		Currency:             "USD",
	},
	"anthropic/claude-3-5-haiku-20241022": {
		Provider:             "anthropic",
		Model:                "claude-3-5-haiku-20241022",
		InputPricePerMToken:  0.80,
		OutputPricePerMToken: 4.00,
		Currency:             "USD",
	},
	"anthropic/claude-3-haiku-20240307": {
		Provider:             "anthropic",
		Model:                "claude-3-haiku-20240307",
		InputPricePerMToken:  0.25,
		OutputPricePerMToken: 1.25,
		Currency:             "USD",
	},
	"anthropic/claude-3-opus-20240229": {
		Provider:             "anthropic",
		Model:                "claude-3-opus-20240229",
		InputPricePerMToken:  15.00,
		OutputPricePerMToken: 75.00,
		Currency:             "USD",
	},
	"openai/gpt-4o": {
		Provider:             "openai",
		Model:                "gpt-4o",
		InputPricePerMToken:  2.50,
		OutputPricePerMToken: 10.00,
		Currency:             "USD",
	},
	"openai/gpt-4o-mini": {
		Provider:             "openai",
		Model:                "gpt-4o-mini",
		InputPricePerMToken:  0.150,
		OutputPricePerMToken: 0.600,
		Currency:             "USD",
	},
	"openai/gpt-4-turbo": {
		Provider:             "openai",
		Model:                "gpt-4-turbo",
		InputPricePerMToken:  10.00,
		OutputPricePerMToken: 30.00,
		Currency:             "USD",
	},
	"openai/gpt-4": {
		Provider:             "openai",
		Model:                "gpt-4",
		InputPricePerMToken:  30.00,
		OutputPricePerMToken: 60.00,
		Currency:             "USD",
	},
	"openai/gpt-3.5-turbo": {
		Provider:             "openai",
		Model:                "gpt-3.5-turbo",
		InputPricePerMToken:  0.50,
		OutputPricePerMToken: 1.50,
		Currency:             "USD",
	},
	"openai/o1": {
		Provider:             "openai",
		Model:                "o1",
		InputPricePerMToken:  15.00,
		OutputPricePerMToken: 60.00,
		Currency:             "USD",
	},
	"openai/o1-mini": {
		Provider:             "openai",
		Model:                "o1-mini",
		InputPricePerMToken:  3.00,
		OutputPricePerMToken: 12.00,
		Currency:             "USD",
	},
	"openai/o1-preview": {
		Provider:             "openai",
		Model:                "o1-preview",
		InputPricePerMToken:  15.00,
		OutputPricePerMToken: 60.00,
		Currency:             "USD",
	},
	"google/gemini-2.0-flash": {
		Provider:             "google",
		Model:                "gemini-2.0-flash",
		InputPricePerMToken:  0.00,
		OutputPricePerMToken: 0.00,
		Currency:             "USD",
	},
	"google/gemini-1.5-pro": {
		Provider:             "google",
		Model:                "gemini-1.5-pro",
		InputPricePerMToken:  1.25,
		OutputPricePerMToken: 5.00,
		Currency:             "USD",
	},
	"google/gemini-1.5-flash": {
		Provider:             "google",
		Model:                "gemini-1.5-flash",
		InputPricePerMToken:  0.075,
		OutputPricePerMToken: 0.30,
		Currency:             "USD",
	},
	"google/gemini-pro": {
		Provider:             "google",
		Model:                "gemini-pro",
		InputPricePerMToken:  0.50,
		OutputPricePerMToken: 1.50,
		Currency:             "USD",
	},
	"deepseek/deepseek-chat": {
		Provider:             "deepseek",
		Model:                "deepseek-chat",
		InputPricePerMToken:  0.28,
		OutputPricePerMToken: 0.42,
		Currency:             "USD",
	},
	"deepseek/deepseek-reasoner": {
		Provider:             "deepseek",
		Model:                "deepseek-reasoner",
		InputPricePerMToken:  0.28,
		OutputPricePerMToken: 0.42,
		Currency:             "USD",
	},
	"groq/llama-3.3-70b-versatile": {
		Provider:             "groq",
		Model:                "llama-3.3-70b-versatile",
		InputPricePerMToken:  0.59,
		OutputPricePerMToken: 0.79,
		Currency:             "USD",
	},
	"groq/llama-3.1-8b-instant": {
		Provider:             "groq",
		Model:                "llama-3.1-8b-instant",
		InputPricePerMToken:  0.05,
		OutputPricePerMToken: 0.08,
		Currency:             "USD",
	},
	"groq/meta-llama/llama-4-scout-17b-16e-instruct": {
		Provider:             "groq",
		Model:                "meta-llama/llama-4-scout-17b-16e-instruct",
		InputPricePerMToken:  0.11,
		OutputPricePerMToken: 0.34,
		Currency:             "USD",
	},
	"groq/meta-llama/llama-4-maverick-17b-128e-instruct": {
		Provider:             "groq",
		Model:                "meta-llama/llama-4-maverick-17b-128e-instruct",
		InputPricePerMToken:  0.20,
		OutputPricePerMToken: 0.60,
		Currency:             "USD",
	},
	"groq/meta-llama/llama-guard-4-12b": {
		Provider:             "groq",
		Model:                "meta-llama/llama-guard-4-12b",
		InputPricePerMToken:  0.20,
		OutputPricePerMToken: 0.20,
		Currency:             "USD",
	},
	"groq/qwen/qwen3-32b": {
		Provider:             "groq",
		Model:                "qwen/qwen3-32b",
		InputPricePerMToken:  0.29,
		OutputPricePerMToken: 0.59,
		Currency:             "USD",
	},
	"groq/openai/gpt-oss-20b": {
		Provider:             "groq",
		Model:                "openai/gpt-oss-20b",
		InputPricePerMToken:  0.075,
		OutputPricePerMToken: 0.30,
		Currency:             "USD",
	},
	"groq/openai/gpt-oss-safeguard-20b": {
		Provider:             "groq",
		Model:                "openai/gpt-oss-safeguard-20b",
		InputPricePerMToken:  0.075,
		OutputPricePerMToken: 0.30,
		Currency:             "USD",
	},
	"groq/openai/gpt-oss-120b": {
		Provider:             "groq",
		Model:                "openai/gpt-oss-120b",
		InputPricePerMToken:  0.15,
		OutputPricePerMToken: 0.60,
		Currency:             "USD",
	},
	"groq/moonshotai/kimi-k2-instruct-0905": {
		Provider:             "groq",
		Model:                "moonshotai/kimi-k2-instruct-0905",
		InputPricePerMToken:  1.00,
		OutputPricePerMToken: 3.00,
		Currency:             "USD",
	},
	"mistral/mistral-large": {
		Provider:             "mistral",
		Model:                "mistral-large",
		InputPricePerMToken:  2.00,
		OutputPricePerMToken: 6.00,
		Currency:             "USD",
	},
	"mistral/mistral-medium": {
		Provider:             "mistral",
		Model:                "mistral-medium",
		InputPricePerMToken:  2.70,
		OutputPricePerMToken: 8.10,
		Currency:             "USD",
	},
	"mistral/mistral-small": {
		Provider:             "mistral",
		Model:                "mistral-small",
		InputPricePerMToken:  0.20,
		OutputPricePerMToken: 0.60,
		Currency:             "USD",
	},
	"mistral/mistral-7b": {
		Provider:             "mistral",
		Model:                "mistral-7b",
		InputPricePerMToken:  0.25,
		OutputPricePerMToken: 0.25,
		Currency:             "USD",
	},
	"mistral/mixtral-8x7b": {
		Provider:             "mistral",
		Model:                "mixtral-8x7b",
		InputPricePerMToken:  0.70,
		OutputPricePerMToken: 0.70,
		Currency:             "USD",
	},
	"cohere/command-r-plus": {
		Provider:             "cohere",
		Model:                "command-r-plus",
		InputPricePerMToken:  2.50,
		OutputPricePerMToken: 10.00,
		Currency:             "USD",
	},
	"cohere/command-r": {
		Provider:             "cohere",
		Model:                "command-r",
		InputPricePerMToken:  0.50,
		OutputPricePerMToken: 1.50,
		Currency:             "USD",
	},
	"cohere/command": {
		Provider:             "cohere",
		Model:                "command",
		InputPricePerMToken:  1.00,
		OutputPricePerMToken: 2.00,
		Currency:             "USD",
	},
	"cohere/command-light": {
		Provider:             "cohere",
		Model:                "command-light",
		InputPricePerMToken:  0.30,
		OutputPricePerMToken: 0.60,
		Currency:             "USD",
	},
	"ollama/llama3.2": {
		Provider:             "ollama",
		Model:                "llama3.2",
		InputPricePerMToken:  0.0,
		OutputPricePerMToken: 0.0,
		Currency:             "USD",
	},
	"ollama/llama3.1": {
		Provider:             "ollama",
		Model:                "llama3.1",
		InputPricePerMToken:  0.0,
		OutputPricePerMToken: 0.0,
		Currency:             "USD",
	},
	"ollama/codellama": {
		Provider:             "ollama",
		Model:                "codellama",
		InputPricePerMToken:  0.0,
		OutputPricePerMToken: 0.0,
		Currency:             "USD",
	},
	"ollama/mistral": {
		Provider:             "ollama",
		Model:                "mistral",
		InputPricePerMToken:  0.0,
		OutputPricePerMToken: 0.0,
		Currency:             "USD",
	},
	"cloudflare/llama-2-7b": {
		Provider:             "cloudflare",
		Model:                "llama-2-7b",
		InputPricePerMToken:  0.0,
		OutputPricePerMToken: 0.0,
		Currency:             "USD",
	},
	"cloudflare/mistral-7b": {
		Provider:             "cloudflare",
		Model:                "mistral-7b",
		InputPricePerMToken:  0.0,
		OutputPricePerMToken: 0.0,
		Currency:             "USD",
	},
}
