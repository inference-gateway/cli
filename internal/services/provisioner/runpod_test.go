package provisioner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testDriver(t *testing.T, handler http.Handler) *RunPod {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	r := NewRunPod("test-key")
	r.RestURL = srv.URL
	r.GraphQLURL = srv.URL + "/graphql"
	return r
}

func TestRunPodCRUD(t *testing.T) {
	var gotAuth, gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		gotMethod, gotPath = req.Method, req.URL.Path
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/pods":
			_ = json.NewDecoder(req.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(Pod{ID: "abc123", Name: "infer-x", CostPerHr: 1.69})
		case req.Method == http.MethodGet && req.URL.Path == "/pods":
			_ = json.NewEncoder(w).Encode([]Pod{
				{ID: "abc123", Name: "infer-x", DesiredStatus: "RUNNING"},
				{ID: "other", Name: "someone-elses-pod"},
			})
		case req.Method == http.MethodGet && req.URL.Path == "/pods/abc123":
			_ = json.NewEncoder(w).Encode(Pod{ID: "abc123", DesiredStatus: "RUNNING"})
		case req.Method == http.MethodDelete && req.URL.Path == "/pods/abc123":
			w.WriteHeader(http.StatusOK)
		case req.URL.Path == "/graphql":
			_, _ = w.Write([]byte(`{"data":{"gpuTypes":[
				{"id":"H100","displayName":"H100 PCIe","memoryInGb":80,"communityPrice":1.99},
				{"id":"A6000","displayName":"RTX A6000","memoryInGb":48,"communityPrice":0.33},
				{"id":"nope","displayName":"unavailable","memoryInGb":8,"communityPrice":0}]}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	r := testDriver(t, handler)
	ctx := context.Background()

	pod, err := r.Provision(ctx, ProvisionRequest{Name: "infer-x", GPUTypeID: "A6000", Image: "img", DiskGB: 50})
	if err != nil || pod.ID != "abc123" {
		t.Fatalf("Provision: pod=%+v err=%v", pod, err)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if gotBody["imageName"] != "img" || gotBody["containerDiskInGb"] != float64(50) {
		t.Fatalf("create body = %v", gotBody)
	}

	pods, err := r.List(ctx)
	if err != nil || len(pods) != 1 || pods[0].ID != "abc123" {
		t.Fatalf("List should filter to infer- prefixed pods, got %+v err=%v", pods, err)
	}

	if _, err := r.Status(ctx, "abc123"); err != nil {
		t.Fatalf("Status: %v", err)
	}

	if err := r.Destroy(ctx, "abc123"); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/pods/abc123" {
		t.Fatalf("Destroy hit %s %s", gotMethod, gotPath)
	}

}

func TestGPUTypes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"gpuTypes":[
			{"id":"H100","displayName":"H100 PCIe","memoryInGb":80,"communityPrice":1.99},
			{"id":"A6000","displayName":"RTX A6000","memoryInGb":48,"communityPrice":0.33},
			{"id":"nope","displayName":"unavailable","memoryInGb":8,"communityPrice":0}]}}`))
	})
	r := testDriver(t, handler)

	types, err := r.GPUTypes(context.Background())
	if err != nil || len(types) != 2 {
		t.Fatalf("GPUTypes should drop unpriced entries, got %+v err=%v", types, err)
	}
	if types[0].ID != "A6000" {
		t.Fatalf("GPUTypes should sort cheapest first, got %+v", types)
	}
}

func TestWaitReadyReportsDownloadPhase(t *testing.T) {
	probes := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/pods/abc123":
			_ = json.NewEncoder(w).Encode(Pod{ID: "abc123", DesiredStatus: "RUNNING"})
		case "/v1/models":
			probes++
			if probes == 1 {
				w.WriteHeader(http.StatusServiceUnavailable) // llama.cpp still loading the model
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	r := testDriver(t, handler)
	r.ProxyBase = r.RestURL
	r.pollInterval = time.Millisecond

	var msgs []string
	if _, err := r.WaitReady(context.Background(), "abc123", 8080, "tok", func(m string) { msgs = append(msgs, m) }); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	joined := strings.Join(msgs, "\n")
	if !strings.Contains(joined, "downloading/loading model") || !strings.Contains(joined, "elapsed") {
		t.Fatalf("expected download-phase heartbeat, got %q", joined)
	}
}

func TestProxyURL(t *testing.T) {
	p := Pod{ID: "abc123"}
	if got := p.ProxyURL(8080); got != "https://abc123-8080.proxy.runpod.net" {
		t.Fatalf("ProxyURL = %q", got)
	}
}
