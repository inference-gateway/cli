package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	adk "github.com/inference-gateway/adk/types"
)

func freeTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

// startTestServer builds and starts the server on loopback and waits for /health.
func startTestServer(t *testing.T, token string) (base string, done func()) {
	t.Helper()
	settings := Settings{Host: "127.0.0.1", Port: freeTestPort(t), Token: token, Mode: "standard", Version: "test"}
	srv, err := NewServer(settings)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	exited := make(chan error, 1)
	go func() { exited <- srv.Start(ctx) }()

	base = fmt.Sprintf("http://127.0.0.1:%d", settings.Port)
	health := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(10 * time.Second)
	for {
		time.Sleep(50 * time.Millisecond)
		res, rerr := health.Get(base + "/health")
		if rerr == nil {
			_ = res.Body.Close()
			break
		}
		select {
		case startErr := <-exited:
			cancel()
			t.Fatalf("the server did not start: %v", startErr)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("the server never answered /health")
		}
	}
	return base, func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = srv.Stop(stopCtx)
	}
}

func TestServerServesCardAndGuardsA2A(t *testing.T) {
	base, done := startTestServer(t, "test-token")
	defer done()

	res, err := http.Get(base + "/.well-known/agent-card.json")
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var card adk.AgentCard
	require.NoError(t, json.NewDecoder(res.Body).Decode(&card))
	require.Equal(t, "infer", card.Name)
	require.NotEmpty(t, card.SecurityRequirements, "the card requires the bearer scheme")
	require.Contains(t, card.SecuritySchemes, "bearer")

	for name, token := range map[string]string{"no token": "", "wrong token": "Bearer nope"} {
		t.Run(name, func(t *testing.T) {
			req, rerr := http.NewRequest(http.MethodPost, base+"/a2a", nil)
			require.NoError(t, rerr)
			if token != "" {
				req.Header.Set("Authorization", token)
			}
			resp, perr := http.DefaultClient.Do(req)
			require.NoError(t, perr)
			defer func() { _ = resp.Body.Close() }()
			require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})
	}

	t.Run("a valid token passes the guard", func(t *testing.T) {
		req, rerr := http.NewRequest(http.MethodPost, base+"/a2a", nil)
		require.NoError(t, rerr)
		req.Header.Set("Authorization", "Bearer test-token")
		resp, perr := http.DefaultClient.Do(req)
		require.NoError(t, perr)
		defer func() { _ = resp.Body.Close() }()
		require.NotEqual(t, http.StatusUnauthorized, resp.StatusCode)
	})
}
