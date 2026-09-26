package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const catalogFixture = `{
  "version": 1,
  "agents": [
    {
      "apiVersion": "adl.inference-gateway.com/v1",
      "kind": "Agent",
      "metadata": {"name": "grafana-agent", "version": "0.3.9"},
      "spec": {
        "server": {"scheme": "http", "port": 8080},
        "scm": {"url": "https://github.com/inference-gateway/grafana-agent"}
      }
    },
    {
      "apiVersion": "adl.inference-gateway.com/v1",
      "kind": "Agent",
      "metadata": {"name": "pinned-agent", "version": "1.2.3"},
      "spec": {
        "server": {"scheme": "https", "port": 8443},
        "deployment": {"cloudrun": {"image": {"registry": "quay.io", "repository": "acme/pinned", "tag": "v2"}}}
      }
    },
    {
      "apiVersion": "adl.inference-gateway.com/v1",
      "kind": "Agent",
      "metadata": {"name": "third-party-agent", "version": "0.1.0"},
      "spec": {
        "server": {"port": 8080},
        "scm": {"url": "https://github.com/acme/third-party-agent"}
      }
    }
  ]
}`

func serveCatalog(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestResolveCatalogAgentFrom(t *testing.T) {
	srv := serveCatalog(t, catalogFixture)
	ctx := context.Background()

	t.Run("inference-gateway agent derives ghcr image from scm", func(t *testing.T) {
		defaults := resolveCatalogAgentFrom(ctx, srv.URL, "grafana-agent")
		if defaults == nil {
			t.Fatal("expected defaults for grafana-agent, got nil")
		}
		if !strings.HasPrefix(defaults.URL, "http://localhost:") {
			t.Fatalf("URL = %q, want http://localhost:<port>", defaults.URL)
		}
		if defaults.OCI != "ghcr.io/inference-gateway/grafana-agent:0.3.9" {
			t.Fatalf("OCI = %q, want ghcr.io/inference-gateway/grafana-agent:0.3.9", defaults.OCI)
		}
		if !defaults.Run {
			t.Fatal("Run = false, want true for an agent with a derivable image")
		}
	})

	t.Run("deployment-declared image wins", func(t *testing.T) {
		defaults := resolveCatalogAgentFrom(ctx, srv.URL, "pinned-agent")
		if defaults == nil {
			t.Fatal("expected defaults for pinned-agent, got nil")
		}
		if defaults.OCI != "quay.io/acme/pinned:v2" {
			t.Fatalf("OCI = %q, want quay.io/acme/pinned:v2", defaults.OCI)
		}
		if !strings.HasPrefix(defaults.URL, "https://localhost:") {
			t.Fatalf("URL = %q, want https://localhost:<port>", defaults.URL)
		}
	})

	t.Run("third-party agent gets URL only", func(t *testing.T) {
		defaults := resolveCatalogAgentFrom(ctx, srv.URL, "third-party-agent")
		if defaults == nil {
			t.Fatal("expected defaults for third-party-agent, got nil")
		}
		if defaults.OCI != "" {
			t.Fatalf("OCI = %q, want empty for a non-inference-gateway source", defaults.OCI)
		}
		if defaults.Run {
			t.Fatal("Run = true, want false for a remote-only agent")
		}
	})

	t.Run("unknown name returns nil", func(t *testing.T) {
		if defaults := resolveCatalogAgentFrom(ctx, srv.URL, "nope-agent"); defaults != nil {
			t.Fatalf("expected nil for unknown name, got %+v", defaults)
		}
	})

	t.Run("unreachable catalog returns nil", func(t *testing.T) {
		closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		closed.Close()
		if defaults := resolveCatalogAgentFrom(ctx, closed.URL, "grafana-agent"); defaults != nil {
			t.Fatalf("expected nil for unreachable catalog, got %+v", defaults)
		}
	})
}
