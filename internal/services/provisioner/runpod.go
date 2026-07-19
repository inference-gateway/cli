// Package provisioner rents on-demand GPU instances running llama.cpp and
// exposes them as a llamacpp provider endpoint (issue #939). RunPod is the
// first driver; the surface is plain CRUD on instances.
package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// PodNamePrefix tags every pod created by infer so List can find them.
const PodNamePrefix = "infer-"

// Pod is the subset of RunPod's Pod object the CLI cares about.
type Pod struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Image         string  `json:"image"`
	DesiredStatus string  `json:"desiredStatus"`
	CostPerHr     float64 `json:"costPerHr"`
	LastStartedAt string  `json:"lastStartedAt"`
}

// ProxyURL is the pod's public HTTPS endpoint for the given internal port.
func (p Pod) ProxyURL(port int) string {
	return fmt.Sprintf("https://%s-%d.proxy.runpod.net", p.ID, port)
}

// GPUType is a rentable GPU with live pricing.
type GPUType struct {
	ID             string  `json:"id"`
	DisplayName    string  `json:"displayName"`
	MemoryInGb     int     `json:"memoryInGb"`
	CommunityPrice float64 `json:"communityPrice"`
	SecurePrice    float64 `json:"securePrice"`
}

// ProvisionRequest describes the pod to create.
type ProvisionRequest struct {
	Name      string
	GPUTypeID string
	Image     string
	StartCmd  []string
	CloudType string // COMMUNITY or SECURE
	DiskGB    int
	Env       map[string]string
	Ports     []string
}

// RunPod is the RunPod driver. Base URLs are fields so tests can point at httptest.
type RunPod struct {
	APIKey     string
	RestURL    string // default https://rest.runpod.io/v1
	GraphQLURL string // default https://api.runpod.io/graphql
	ProxyBase  string // overrides Pod.ProxyURL in WaitReady; tests only
	Client     *http.Client

	pollInterval time.Duration // WaitReady probe interval; 0 means 10s (short in tests)
}

func NewRunPod(apiKey string) *RunPod {
	return &RunPod{
		APIKey:     apiKey,
		RestURL:    "https://rest.runpod.io/v1",
		GraphQLURL: "https://api.runpod.io/graphql",
		Client:     &http.Client{Timeout: 60 * time.Second},
	}
}

func (r *RunPod) do(ctx context.Context, method, url string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("runpod api: %s %s: %s: %s", method, url, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Provision creates a pod. CRUD: create.
func (r *RunPod) Provision(ctx context.Context, req ProvisionRequest) (Pod, error) {
	payload := map[string]any{
		"name":              req.Name,
		"imageName":         req.Image,
		"dockerStartCmd":    req.StartCmd,
		"ports":             req.Ports,
		"gpuTypeIds":        []string{req.GPUTypeID},
		"cloudType":         req.CloudType,
		"containerDiskInGb": req.DiskGB,
	}
	if len(req.Env) > 0 {
		payload["env"] = req.Env
	}
	var pod Pod
	err := r.do(ctx, http.MethodPost, r.RestURL+"/pods", payload, &pod)
	return pod, err
}

// List returns pods created by infer (name-prefix filtered). CRUD: read.
func (r *RunPod) List(ctx context.Context) ([]Pod, error) {
	var pods []Pod
	if err := r.do(ctx, http.MethodGet, r.RestURL+"/pods", nil, &pods); err != nil {
		return nil, err
	}
	mine := pods[:0]
	for _, p := range pods {
		if strings.HasPrefix(p.Name, PodNamePrefix) {
			mine = append(mine, p)
		}
	}
	return mine, nil
}

// Status returns a single pod. CRUD: read.
func (r *RunPod) Status(ctx context.Context, id string) (Pod, error) {
	var pod Pod
	err := r.do(ctx, http.MethodGet, r.RestURL+"/pods/"+id, nil, &pod)
	return pod, err
}

// Destroy terminates a pod; billing stops. CRUD: delete.
func (r *RunPod) Destroy(ctx context.Context, id string) error {
	return r.do(ctx, http.MethodDelete, r.RestURL+"/pods/"+id, nil, nil)
}

// GPUTypes returns rentable GPU types with live pricing, cheapest first.
// This is the one non-REST call (RunPod exposes pricing only via GraphQL).
func (r *RunPod) GPUTypes(ctx context.Context) ([]GPUType, error) {
	query := map[string]string{
		"query": `query { gpuTypes { id displayName memoryInGb communityPrice securePrice } }`,
	}
	var out struct {
		Data struct {
			GPUTypes []GPUType `json:"gpuTypes"`
		} `json:"data"`
	}
	if err := r.do(ctx, http.MethodPost, r.GraphQLURL, query, &out); err != nil {
		return nil, err
	}
	types := out.Data.GPUTypes[:0]
	for _, t := range out.Data.GPUTypes {
		if t.CommunityPrice > 0 || t.SecurePrice > 0 {
			types = append(types, t)
		}
	}
	sort.Slice(types, func(i, j int) bool { return price(types[i]) < price(types[j]) })
	return types, nil
}

func price(t GPUType) float64 {
	if t.CommunityPrice > 0 {
		return t.CommunityPrice
	}
	return t.SecurePrice
}

// WaitReady polls the pod, then the llama.cpp /health endpoint through the
// proxy until the model answers. /health is unauthenticated in llama.cpp, so
// this needs no per-session token and works for `gpu status --wait` too.
func (r *RunPod) WaitReady(ctx context.Context, id string, port int, report func(string)) (Pod, error) {
	var pod Pod
	for {
		var err error
		pod, err = r.Status(ctx, id)
		if err != nil {
			return pod, err
		}
		if pod.DesiredStatus == "RUNNING" {
			break
		}
		report("waiting for pod to start (" + pod.DesiredStatus + ")...")
		if err := sleep(ctx, 5*time.Second); err != nil {
			return pod, err
		}
	}
	base := pod.ProxyURL(port)
	if r.ProxyBase != "" {
		base = r.ProxyBase
	}
	url := base + "/health"
	report("pod running; waiting for llama.cpp to download the model and answer...")
	start := time.Now()
	for {
		phase := "starting llama.cpp server..."
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := r.Client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return pod, nil
			}
			if resp.StatusCode == http.StatusServiceUnavailable {
				phase = "downloading/loading model..."
			}
		}
		report(phase + " (" + time.Since(start).Round(time.Second).String() + " elapsed)")
		interval := r.pollInterval
		if interval == 0 {
			interval = 10 * time.Second
		}
		if err := sleep(ctx, interval); err != nil {
			return pod, err
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
