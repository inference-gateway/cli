package runpod

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	computedomain "github.com/inference-gateway/cli/internal/compute/domain"
)

// pod is the part of RunPod's Pod object infer reads, as RunPod spells it.
type pod struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Image         string  `json:"image"`
	DesiredStatus string  `json:"desiredStatus"`
	CostPerHr     float64 `json:"costPerHr"`
	LastStartedAt string  `json:"lastStartedAt"`
}

func (p pod) instance() computedomain.GPUInstance {
	return computedomain.GPUInstance{ID: p.ID, Name: p.Name, Image: p.Image, Status: p.DesiredStatus, HourlyCost: p.CostPerHr, StartedAt: p.LastStartedAt}
}

// gpuType is a GPU type as RunPod's GraphQL API spells it.
type gpuType struct {
	ID             string  `json:"id"`
	DisplayName    string  `json:"displayName"`
	MemoryInGb     int     `json:"memoryInGb"`
	CommunityPrice float64 `json:"communityPrice"`
	SecurePrice    float64 `json:"securePrice"`
}

func (t gpuType) asGPUType() computedomain.GPUType {
	return computedomain.GPUType{ID: t.ID, DisplayName: t.DisplayName, MemoryGB: t.MemoryInGb, CommunityPrice: t.CommunityPrice, SecurePrice: t.SecurePrice}
}

// URL is the public HTTPS endpoint RunPod proxies to the instance's port.
func (r *RunPod) URL(id string, port int) string {
	return fmt.Sprintf("https://%s-%d.proxy.runpod.net", id, port)
}

// RunPod rents GPU instances from RunPod and translates its API into compute's
// domain. Base URLs are fields so tests can point at httptest.
type RunPod struct {
	APIKey     string
	RestURL    string // default https://rest.runpod.io/v1
	GraphQLURL string // default https://api.runpod.io/graphql
	ProxyBase  string // overrides URL in WaitReady; tests only
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

// Provision rents a GPU instance. CRUD: create.
func (r *RunPod) Provision(ctx context.Context, req computedomain.ProvisionRequest) (computedomain.GPUInstance, error) {
	payload := map[string]any{
		"name":              req.Name,
		"imageName":         req.Image,
		"dockerStartCmd":    req.StartCmd,
		"ports":             req.Ports,
		"gpuTypeIds":        []string{req.GPUTypeID},
		"cloudType":         string(req.Cloud),
		"containerDiskInGb": req.DiskGB,
	}
	if len(req.Env) > 0 {
		payload["env"] = req.Env
	}
	var created pod
	err := r.do(ctx, http.MethodPost, r.RestURL+"/pods", payload, &created)
	return created.instance(), err
}

// List returns the instances infer rented, told by their name prefix. CRUD: read.
func (r *RunPod) List(ctx context.Context) ([]computedomain.GPUInstance, error) {
	var pods []pod
	if err := r.do(ctx, http.MethodGet, r.RestURL+"/pods", nil, &pods); err != nil {
		return nil, err
	}
	var mine []computedomain.GPUInstance
	for _, p := range pods {
		if strings.HasPrefix(p.Name, computedomain.InstanceNamePrefix) {
			mine = append(mine, p.instance())
		}
	}
	return mine, nil
}

// Status returns a single instance. CRUD: read.
func (r *RunPod) Status(ctx context.Context, id string) (computedomain.GPUInstance, error) {
	var found pod
	err := r.do(ctx, http.MethodGet, r.RestURL+"/pods/"+id, nil, &found)
	return found.instance(), err
}

// Destroy terminates an instance, and billing stops. CRUD: delete.
func (r *RunPod) Destroy(ctx context.Context, id string) error {
	return r.do(ctx, http.MethodDelete, r.RestURL+"/pods/"+id, nil, nil)
}

// GPUTypes returns rentable GPU types with live pricing, cheapest first.
// This is the one non-REST call (RunPod exposes pricing only via GraphQL).
func (r *RunPod) GPUTypes(ctx context.Context) ([]computedomain.GPUType, error) {
	query := map[string]string{
		"query": `query { gpuTypes { id displayName memoryInGb communityPrice securePrice } }`,
	}
	var out struct {
		Data struct {
			GPUTypes []gpuType `json:"gpuTypes"`
		} `json:"data"`
	}
	if err := r.do(ctx, http.MethodPost, r.GraphQLURL, query, &out); err != nil {
		return nil, err
	}
	var types []computedomain.GPUType
	for _, t := range out.Data.GPUTypes {
		if gpu := t.asGPUType(); gpu.Rentable() {
			types = append(types, gpu)
		}
	}
	slices.SortStableFunc(types, func(a, b computedomain.GPUType) int {
		priceA, _ := a.HourlyPrice()
		priceB, _ := b.HourlyPrice()
		return cmp.Compare(priceA, priceB)
	})
	return types, nil
}

// WaitReady polls the pod, then the llama.cpp /health endpoint through the
// proxy until the model answers. /health is unauthenticated in llama.cpp, so
// this needs no per-session token and works for `gpu status --wait` too.
func (r *RunPod) WaitReady(ctx context.Context, id string, port int, report func(string)) (computedomain.GPUInstance, error) {
	var instance computedomain.GPUInstance
	for {
		var err error
		instance, err = r.Status(ctx, id)
		if err != nil {
			return instance, err
		}
		if instance.Running() {
			break
		}
		report("waiting for pod to start (" + instance.Status + ")...")
		if err := sleep(ctx, 5*time.Second); err != nil {
			return instance, err
		}
	}
	base := r.URL(instance.ID, port)
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
				return instance, nil
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
			return instance, err
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
