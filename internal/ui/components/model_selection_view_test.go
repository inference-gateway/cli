package components

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	"github.com/stretchr/testify/assert"
)

// newFilterTestSelector builds a selector backed by a fake pricing service with
// three representative models: a per-token (pay-as-you-go) model, a genuinely
// free model, and a subscription-gated model ($0/$0 but gated).
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
		return model == "subscription-model"
	}
	pricing.FormatModelPricingStub = func(model string) string {
		switch model {
		case "paid-model":
			return "$3.00/$15.00 per MTok"
		case "free-model", "subscription-model":
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
// and exhaustive: free / pay-as-you-go / subscription are mutually exclusive and
// All is the union.
func TestModelSelector_FilterBuckets(t *testing.T) {
	models := []string{"paid-model", "free-model", "subscription-model"}

	assert.ElementsMatch(t, models, filteredFor(ModelViewAll, models))
	assert.ElementsMatch(t, []string{"free-model"}, filteredFor(ModelViewFree, models))
	assert.ElementsMatch(t, []string{"paid-model"}, filteredFor(ModelViewPayAsYouGo, models))
	assert.ElementsMatch(t, []string{"subscription-model"}, filteredFor(ModelViewSubscription, models))
}

// TestModelSelector_SubscriptionModelExcludedFromFree is the core bug fix for
// issue #590: a subscription model is $0/$0 but must never be shown under the
// Free tab.
func TestModelSelector_SubscriptionModelExcludedFromFree(t *testing.T) {
	models := []string{"subscription-model"}

	assert.NotContains(t, filteredFor(ModelViewFree, models), "subscription-model")
	assert.Contains(t, filteredFor(ModelViewSubscription, models), "subscription-model")
}

// TestModelSelector_FormatModelSuffixSubscription checks the per-row marker: a
// subscription model shows "subscription" and suppresses the misleading "free"
// token, while a genuinely free model still shows "free".
func TestModelSelector_FormatModelSuffixSubscription(t *testing.T) {
	m := newFilterTestSelector([]string{"subscription-model", "free-model"})

	subSuffix := m.formatModelSuffix("subscription-model")
	assert.Contains(t, subSuffix, "subscription")
	assert.NotContains(t, subSuffix, "free")

	freeSuffix := m.formatModelSuffix("free-model")
	assert.Contains(t, freeSuffix, "free")
	assert.NotContains(t, freeSuffix, "subscription")
}
