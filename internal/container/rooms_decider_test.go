package container

import (
	"context"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	decisiondomain "github.com/inference-gateway/cli/internal/decision/domain"
)

func TestRoomsDeciderReportsAnUnreachableGateway(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Gateway.Run = false
	cfg.Gateway.URL = "http://127.0.0.1:1"

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := NewRoomsDecider(cfg).Decide(ctx, "anthropic/claude-haiku-5-5", "evidence",
		[]decisiondomain.Question{{Name: "tester", Type: decisiondomain.Predicate}})
	require.Error(t, err, "a decision the gateway cannot answer fails, so the post is logged routed to nobody")
}

func TestRoomsDeciderAnswersFromTheMockGatewayInMockMode(t *testing.T) {
	t.Setenv("INFER_GATEWAY_MOCK_SCENARIOS", "")
	cfg := config.DefaultConfig()
	cfg.Gateway.Mock = true
	cfg.Gateway.Run = false
	cfg.Gateway.URL = "http://127.0.0.1:1"

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	decisions, err := NewRoomsDecider(cfg).Decide(ctx, "mock/ollama_cloud/clef:27b", "evidence",
		[]decisiondomain.Question{{Name: "tester", Type: decisiondomain.Predicate}})
	require.NoError(t, err, "mock mode never reaches the configured gateway")
	answer, ok := decisions.ByName("tester")
	require.True(t, ok)
	require.Positive(t, answer.Probability)
}
