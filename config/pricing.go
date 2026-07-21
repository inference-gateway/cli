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
	// RequiresPro marks a model as gated behind a paid Pro subscription
	// (e.g. some Ollama Cloud models). Such models have no per-token price
	// but are not freely available. Omitting this in a custom entry resets
	// it to false, overriding any default flag for that model.
	RequiresPro bool `yaml:"requires_pro" mapstructure:"requires_pro"`
}

// GetDefaultPricingConfig returns the default pricing configuration.
func GetDefaultPricingConfig() PricingConfig {
	return PricingConfig{
		Enabled:      true,
		Currency:     "USD",
		CustomPrices: make(map[string]CustomPricing),
	}
}
