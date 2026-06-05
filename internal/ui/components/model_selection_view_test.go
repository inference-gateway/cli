package components

import (
	"testing"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	"github.com/stretchr/testify/assert"
)

// newFilterTestSelector builds a selector backed by a fake pricing service with
// three representative models: a per-token paid model, a genuinely free model,
// and a Pro-subscription model ($0/$0 but gated).
func newFilterTestSelector(models []string) *ModelSelectorImpl {
	pricing := &domainmocks.FakePricingService{}
	pricing.IsEnabledReturns(true)
	pricing.GetInputPriceStub = func(model string) float64 {
		if model == "paid-model" {
			return 3.0
		}
		return 0.0
	}
	pricing.GetOutputPriceStub = func(model string) float64 {
		if model == "paid-model" {
			return 15.0
		}
		return 0.0
	}
	pricing.RequiresProStub = func(model string) bool {
		return model == "pro-model"
	}
	pricing.FormatModelPricingStub = func(model string) string {
		switch model {
		case "paid-model":
			return "$3.00/$15.00 per MTok"
		case "free-model", "pro-model":
			return "free"
		default:
			return ""
		}
	}
	return NewModelSelector(models, nil, pricing, nil, nil)
}

func filteredFor(view ModelViewMode, models []string) []string {
	m := newFilterTestSelector(models)
	m.currentView = view
	m.applyFilters()
	return m.filteredModels
}

// TestModelSelector_FilterBuckets verifies the four filter views are disjoint
// and exhaustive: free / paid / pro are mutually exclusive and All is the union.
func TestModelSelector_FilterBuckets(t *testing.T) {
	models := []string{"paid-model", "free-model", "pro-model"}

	assert.ElementsMatch(t, models, filteredFor(ModelViewAll, models))
	assert.ElementsMatch(t, []string{"free-model"}, filteredFor(ModelViewFree, models))
	assert.ElementsMatch(t, []string{"paid-model"}, filteredFor(ModelViewPaid, models))
	assert.ElementsMatch(t, []string{"pro-model"}, filteredFor(ModelViewPro, models))
}

// TestModelSelector_ProModelExcludedFromFree is the core bug fix for issue #590:
// a Pro model is $0/$0 but must never be shown under the Free tab.
func TestModelSelector_ProModelExcludedFromFree(t *testing.T) {
	models := []string{"pro-model"}

	assert.NotContains(t, filteredFor(ModelViewFree, models), "pro-model")
	assert.Contains(t, filteredFor(ModelViewPro, models), "pro-model")
}

// TestModelSelector_FormatModelSuffixPro checks the per-row marker: a Pro model
// shows "pro subscription" and suppresses the misleading "free" token, while a
// genuinely free model still shows "free".
func TestModelSelector_FormatModelSuffixPro(t *testing.T) {
	m := newFilterTestSelector([]string{"pro-model", "free-model"})

	proSuffix := m.formatModelSuffix("pro-model")
	assert.Contains(t, proSuffix, "pro subscription")
	assert.NotContains(t, proSuffix, "free")

	freeSuffix := m.formatModelSuffix("free-model")
	assert.Contains(t, freeSuffix, "free")
	assert.NotContains(t, freeSuffix, "pro subscription")
}
