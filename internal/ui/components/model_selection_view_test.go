package components

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	assert "github.com/stretchr/testify/assert"
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
	return NewModelSelector(models, nil, pricing, nil, createMockStyleProvider())
}

func filteredFor(view ModelViewMode, models []string) []string {
	m := newFilterTestSelector(models)
	m.currentView = view
	return m.tabModels()
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

// TestModelSelector_EnterSelectsAndEmitsEvent drives the huh select to
// completion and asserts the selection reaches the model service and the
// ModelSelectedEvent carries the chosen model.
func TestModelSelector_EnterSelectsAndEmitsEvent(t *testing.T) {
	ms := &domainmocks.FakeModelService{}
	pricing := &domainmocks.FakePricingService{}
	m := NewModelSelector([]string{"model-a", "model-b"}, ms, pricing, nil, createMockStyleProvider())

	var selected string
	var pump func(msg tea.Msg)
	pump = func(msg tea.Msg) {
		model, cmd := m.Update(msg)
		m = model.(*ModelSelectorImpl)
		for cmd != nil {
			out := cmd()
			if out == nil {
				return
			}
			if batch, ok := out.(tea.BatchMsg); ok {
				for _, c := range batch {
					if c != nil {
						pump(c())
					}
				}
				return
			}
			if ev, ok := out.(domain.ModelSelectedEvent); ok {
				selected = ev.Model
				return
			}
			model, cmd = m.Update(out)
			m = model.(*ModelSelectorImpl)
		}
	}

	pump(tea.KeyPressMsg{Code: tea.KeyDown})
	pump(tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, "model-b", selected)
	assert.True(t, m.IsSelected())
	assert.Equal(t, "model-b", m.GetSelected())
	assert.Equal(t, 1, ms.SelectModelCallCount())
	assert.Equal(t, "model-b", ms.SelectModelArgsForCall(0))
}

// typeString feeds a string into the selector one printable key at a time.
func typeString(m *ModelSelectorImpl, s string) {
	for _, r := range s {
		model, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		*m = *model.(*ModelSelectorImpl)
	}
}

// TestModelSelector_SearchFiltersByNameOnly verifies the / search narrows the
// option set by model name, ignoring the metadata suffix ("free" etc.), and
// that esc clears the query and restores the full list.
func TestModelSelector_SearchFiltersByNameOnly(t *testing.T) {
	m := newFilterTestSelector([]string{"paid-model", "free-model", "subscription-model"})

	typeString(m, "/")
	assert.True(t, m.searchMode)

	// "free" appears in paid-model's suffix via pricing labels but must only
	// match the model whose NAME contains it.
	typeString(m, "free")
	assert.Equal(t, []string{"free-model"}, m.visibleModels())

	// No name matches "per MTok" even though every paid suffix contains it.
	typeString(m, "x")
	assert.Empty(t, m.visibleModels())

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.False(t, m.searchMode)
	assert.Equal(t, "", m.search.Value())
	assert.Len(t, m.visibleModels(), 3)
}

// TestModelSelector_SearchEnterSelectsFilteredMatch drives search + Enter and
// asserts the emitted event carries the filtered match, not an index into the
// unfiltered list.
func TestModelSelector_SearchEnterSelectsFilteredMatch(t *testing.T) {
	ms := &domainmocks.FakeModelService{}
	pricing := &domainmocks.FakePricingService{}
	m := NewModelSelector([]string{"alpha", "beta", "gamma"}, ms, pricing, nil, createMockStyleProvider())

	typeString(m, "/gam")

	var selected string
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for cmd != nil {
		out := cmd()
		if out == nil {
			break
		}
		if ev, ok := out.(domain.ModelSelectedEvent); ok {
			selected = ev.Model
			break
		}
		_, cmd = m.Update(out)
	}

	assert.Equal(t, "gamma", selected)
	assert.Equal(t, "gamma", m.GetSelected())
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
