package web_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	web "github.com/inference-gateway/cli/internal/presentation/web"
)

func TestScreenPOSTTakesTheScreenOverAndGuardsStaleInputs(t *testing.T) {
	var controls []string
	var inputs []web.ScreenInput
	server := httptest.NewServer(&web.Screen{
		Token: "secret",
		Control: func(_ context.Context, id, action string) (*web.ScreenOutcome, error) {
			controls = append(controls, id+":"+action)
			switch id + ":" + action {
			case "held:take_over":
				return &web.ScreenOutcome{Held: true, Error: "the screen is already held by the person"}, nil
			case "gone:take_over":
				return nil, errors.New("session gone is not open in /tmp")
			case "bad:take_over":
				return nil, errors.New("the session worker did not answer")
			}
			return &web.ScreenOutcome{Held: action == web.ScreenActionTakeOver}, nil
		},
		Input: func(_ context.Context, id string, input web.ScreenInput) (*web.ScreenOutcome, error) {
			inputs = append(inputs, input)
			switch input.Kind {
			case "scroll":
				return &web.ScreenOutcome{Error: "the screen is not held"}, nil
			case "move":
				return &web.ScreenOutcome{Held: true, Error: "the display refused the input"}, nil
			}
			return &web.ScreenOutcome{Held: true}, nil
		},
	})
	defer server.Close()

	post := func(t *testing.T, payload, token, id string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, server.URL+web.ScreenPath+"?id="+id, strings.NewReader(payload))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	if resp := post(t, `{"action":"take_over"}`, "", "s1"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 for a POST without the bearer, got %d", resp.StatusCode)
	}
	if resp := post(t, `{"action":"take_over"}`, "secret", "s1"); resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 for a take over, got %d", resp.StatusCode)
	}
	if resp := post(t, `{"action":"hand_back"}`, "secret", "s1"); resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 for a hand back, got %d", resp.StatusCode)
	}
	if resp := post(t, `{"action":"seize"}`, "secret", "s1"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for an unknown action, got %d", resp.StatusCode)
	}
	if resp := post(t, `{"input":{"kind":"click","x":5,"y":6,"button":"right"}}`, "secret", "s1"); resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 for a held-screen input, got %d", resp.StatusCode)
	}
	if len(controls) != 2 || controls[0] != "s1:take_over" || controls[1] != "s1:hand_back" {
		t.Fatalf("the handler took control calls %v, want s1 take over and hand back", controls)
	}
	if len(inputs) != 1 || inputs[0].Kind != "click" || inputs[0].X != 5 || inputs[0].Button != "right" {
		t.Fatalf("the handler drove inputs %+v, want the right click at (5, 6)", inputs)
	}
	if resp := post(t, `{"action":"take_over"}`, "secret", "held"); resp.StatusCode != http.StatusConflict {
		t.Fatalf("want 409 for a screen somebody else holds, got %d", resp.StatusCode)
	}
	if resp := post(t, `{"input":{"kind":"scroll"}}`, "secret", "s1"); resp.StatusCode != http.StatusConflict {
		t.Fatalf("want 409 for an input on a screen nobody holds, got %d", resp.StatusCode)
	}
	if resp := post(t, `{"input":{"kind":"move"}}`, "secret", "s1"); resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("want 500 for a refused drive, got %d", resp.StatusCode)
	}
	if resp := post(t, `{"action":"take_over"}`, "secret", "gone"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for a session that is not open, got %d", resp.StatusCode)
	}
	if resp := post(t, `{"action":"take_over"}`, "secret", "bad"); resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502 for a relay that did not answer, got %d", resp.StatusCode)
	}
	var answer struct {
		Held bool `json:"held"`
	}
	if json.NewDecoder(post(t, `{"action":"take_over"}`, "secret", "s1").Body).Decode(&answer) != nil || !answer.Held {
		t.Fatalf("the take-over answer did not read held true: %+v", answer)
	}
}
