# Cost Tracking

[← Back to README](../README.md)

**What** - real-time estimates of what each chat session costs, per model and in aggregate.
**Why** - API spend is easy to lose track of, and a per-model breakdown shows which model is actually driving the bill.
**How** - costs are computed from token usage times a per-model price table; view them with
`/cost`, and override or add prices in `config.yaml` when the built-in table does not fit.

## Viewing Costs

Use the `/cost` command in any chat session to see the cost breakdown:

```bash
# In chat, use the /cost shortcut
/cost
```

This displays:

- **Total session cost** in USD
- **Input/output costs** separately
- **Per-model breakdown** when using multiple models
- **Token usage** for each model

**Status Bar**: Session costs are also displayed in the status bar (e.g., `$0.0234`) if enabled, once the cost is above zero.

## Configuring Pricing

Prices come from the gateway's `/models` listing, and `pricing.custom_prices` entries take
precedence over them. A model with neither is unpriced.

The model picker groups models into three categories you can filter with the
`[1] All` / `[2] Free` / `[3] Pay-as-you-go` / `[4] Subscription` tabs:

- **Free** - no per-token cost (e.g. local Ollama, Gemma).
- **Pay-as-you-go** - billed per token at the listed `$input/$output per MTok` rate.
- **Subscription** - gated behind a paid subscription (some Ollama Cloud models).
  These have no per-token price but are not free, so they are marked
  `subscription` instead of `free` to avoid the misleading label.

**Override pricing** for specific models or add pricing for custom models:

```yaml
# .infer/config.yaml
pricing:
  enabled: true
  currency: "USD"
  custom_prices:
    # Override existing model pricing
    "openai/gpt-4o":
      input_price_per_mtoken: 2.50    # Price per million input tokens
      output_price_per_mtoken: 10.00  # Price per million output tokens

    # Add pricing for custom/local models
    "ollama/llama3.2":
      input_price_per_mtoken: 0.0
      output_price_per_mtoken: 0.0

    "custom-fine-tuned-model":
      input_price_per_mtoken: 5.00
      output_price_per_mtoken: 15.00

    # Mark a model as subscription-only (no per-token cost, but gated)
    "ollama_cloud/deepseek-v4-pro":
      input_price_per_mtoken: 0.0
      output_price_per_mtoken: 0.0
      requires_pro: true
```

> **Note:** A custom entry fully replaces the default for that model. Omitting
> `requires_pro` in a custom override resets it to `false`, so re-state
> `requires_pro: true` if you override a model the gateway flags as subscription-only.

**Via environment variables:**

```bash
# Disable cost tracking entirely
export INFER_PRICING_ENABLED=false

# Hide cost from status bar
export INFER_CHAT_STATUS_BAR_INDICATORS_COST=false
```

> **Note:** `pricing.custom_prices` is a map keyed by `provider/model`, so its entries
> cannot be set through environment variables. Override per-model pricing only via
> the `custom_prices` block in `config.yaml` (shown above).

**Status Bar Configuration:**

```yaml
# .infer/config.yaml
chat:
  status_bar:
    enabled: true
    indicators:
      cost: true  # Show/hide cost indicator
```

## Cost Calculation

- Costs are calculated as: `(tokens / 1,000,000) × price_per_million_tokens`
- Prices are per million tokens (input and output priced separately)
- Models without pricing data (Ollama, free tiers) cost $0, so the status bar hides the indicator
- Token counts use actual usage from providers or polyfilled estimates

## Related

- [Commands Reference](commands-reference.md) - the `infer stats` and `infer insights` commands
- [Configuration Reference](configuration-reference.md#chat-interface-settings) - status bar options
