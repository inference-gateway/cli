package domain_test

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	domain "github.com/inference-gateway/cli/internal/conversation/domain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
)

func TestFormatModelPricingLabel(t *testing.T) {
	tests := []struct {
		name        string
		pricing     string
		requiresPro bool
		want        string
	}{
		{name: "no pricing, no pro", pricing: "", requiresPro: false, want: ""},
		{name: "priced, no pro", pricing: "$1.00/$2.00 per MTok", requiresPro: false, want: "$1.00/$2.00 per MTok"},
		{name: "free, no pro", pricing: "free", requiresPro: false, want: "free"},
		{name: "no pricing, pro", pricing: "", requiresPro: true, want: "subscription"},
		{name: "free replaced by subscription", pricing: "free", requiresPro: true, want: "subscription"},
		{name: "priced and pro keeps both", pricing: "$1.00/$2.00 per MTok", requiresPro: true, want: "$1.00/$2.00 per MTok, subscription"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &convmocks.FakePricingService{}
			fake.FormatModelPricingReturns(tt.pricing)
			fake.RequiresProReturns(tt.requiresPro)

			assert.Equal(t, tt.want, domain.FormatModelPricingLabel(fake, "prov/model"))
		})
	}
}

func TestFormatModelPricingLabel_NilService(t *testing.T) {
	assert.Empty(t, domain.FormatModelPricingLabel(nil, "prov/model"))
}
