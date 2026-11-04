package web_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	web "github.com/inference-gateway/cli/internal/presentation/web"
)

func TestScreenServesTheSessionsFrame(t *testing.T) {
	var asked string
	server := httptest.NewServer(&web.Screen{Token: "secret", Frame: func(_ context.Context, id string) (*agentdomain.Frame, error) {
		asked = id
		if id == "gone" {
			return nil, errors.New("no live session")
		}
		return &agentdomain.Frame{Data: "anBlZw==", Format: "jpeg", Width: 4, Height: 2}, nil
	}})
	defer server.Close()

	get := func(id, token string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, server.URL+web.ScreenPath+"?id="+id, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	resp := get("s1", "secret")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "jpeg" || resp.Header.Get("Content-Type") != "image/jpeg" || resp.Header.Get("X-Frame-Width") != "4" || asked != "s1" {
		t.Fatalf("want the frame as image/jpeg, got %d %q %v", resp.StatusCode, body, resp.Header)
	}
	if resp := get("s1", "nope"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 for a wrong token, got %d", resp.StatusCode)
	}
	if resp := get("gone", "secret"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 when the session has no frame, got %d", resp.StatusCode)
	}
}
