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
	server := httptest.NewServer(&web.Screen{Token: "secret", Frame: func(_ context.Context, id, _ string) (*agentdomain.Frame, error) {
		asked = id
		switch id {
		case "gone":
			return nil, errors.New("no live session")
		case "svg":
			return &agentdomain.Frame{Data: "anBlZw==", Format: "svg+xml"}, nil
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
	if resp := get("svg", "secret"); resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("a format the screen sources never produce must be served as image/jpeg, got %q", resp.Header.Get("Content-Type"))
	}
	if resp := get("gone", "secret"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 when the session has no frame, got %d", resp.StatusCode)
	}
}

func TestScreenAnswersNotModifiedForTheFrameTheViewerHas(t *testing.T) {
	var haves []string
	server := httptest.NewServer(&web.Screen{Token: "secret", Frame: func(_ context.Context, _, have string) (*agentdomain.Frame, error) {
		haves = append(haves, have)
		if have == "f1" {
			return nil, web.ErrFrameUnchanged
		}
		return &agentdomain.Frame{ID: "f1", Data: "anBlZw==", Format: "jpeg"}, nil
	}})
	defer server.Close()

	get := func(ifNoneMatch string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, server.URL+web.ScreenPath+"?id=s1", nil)
		req.Header.Set("Authorization", "Bearer secret")
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	first := get("")
	tag := first.Header.Get("ETag")
	if first.StatusCode != http.StatusOK || tag != `"f1"` {
		t.Fatalf("want the frame with its id as the ETag, got %d and ETag %q", first.StatusCode, tag)
	}

	again := get(tag)
	body, _ := io.ReadAll(again.Body)
	if again.StatusCode != http.StatusNotModified || len(body) != 0 {
		t.Fatalf("want 304 without a body for the frame the viewer has, got %d with %d bytes", again.StatusCode, len(body))
	}

	if other := get(`"f0"`); other.StatusCode != http.StatusOK {
		t.Fatalf("want the frame for a viewer that has an older one, got %d", other.StatusCode)
	}
	if len(haves) != 3 || haves[0] != "" || haves[1] != "f1" || haves[2] != "f0" {
		t.Fatalf("the frame ids asked with were %q, want none, f1 and f0", haves)
	}
}
