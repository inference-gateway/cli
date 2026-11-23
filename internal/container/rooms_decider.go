package container

import (
	"cmp"
	"context"
	"fmt"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	decisiondomain "github.com/inference-gateway/cli/internal/decision/domain"
	decisiongateway "github.com/inference-gateway/cli/internal/decision/infrastructure/gateway"
	gateway "github.com/inference-gateway/cli/internal/gateway"
	containerruntime "github.com/inference-gateway/cli/internal/platform/container"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// defaultClientTimeoutSeconds is client.timeout when unset.
const defaultClientTimeoutSeconds = 200

// NewRoomsDecider returns the decider the daemon's rooms router asks about a
// post that pings nobody. It starts the gateway, or joins the running one, or
// serves the mock one in mock mode, on its first decision, so a daemon whose
// rooms route by pings alone never does, and it tries again after a failed start.
func NewRoomsDecider(cfg *config.Config) decisiondomain.Decider {
	return &roomsDecider{cfg: cfg}
}

type roomsDecider struct {
	cfg     *config.Config
	mu      sync.Mutex
	decider decisiondomain.Decider
}

func (d *roomsDecider) Decide(ctx context.Context, model, evidence string, questions []decisiondomain.Question) (decisiondomain.Decisions, error) {
	decider, err := d.gatewayDecider()
	if err != nil {
		return decisiondomain.Decisions{}, err
	}
	return decider.Decide(ctx, model, evidence, questions)
}

func (d *roomsDecider) gatewayDecider() (decisiondomain.Decider, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.decider != nil {
		return d.decider, nil
	}
	if d.cfg.Gateway.Mock {
		return d.mockDecider()
	}
	sessionID := convdomain.GenerateSessionID()
	runtime, err := containerruntime.NewContainerRuntime(string(sessionID), containerruntime.RuntimeType(d.cfg.ContainerRuntime.Type))
	if err != nil {
		logger.Warn("rooms start the gateway without a container runtime", "error", err)
	}
	supervisor := gateway.NewSupervisor(sessionID, d.cfg, runtime)
	if err := supervisor.EnsureStarted(); err != nil {
		return nil, err
	}
	return d.newDecider(gatewayBaseURL(d.cfg, supervisor)), nil
}

// mockDecider answers from a mock gateway of its own, since the daemon serves
// none and the workers' mocks live in their own processes.
func (d *roomsDecider) mockDecider() (decisiondomain.Decider, error) {
	_, url, err := serveMockGateway()
	if err != nil {
		return nil, fmt.Errorf("mock gateway mode: %w", err)
	}
	return d.newDecider(url + "/v1"), nil
}

func (d *roomsDecider) newDecider(baseURL string) decisiondomain.Decider {
	d.decider = decisiongateway.New(sdk.NewClient(&sdk.ClientOptions{
		BaseURL:   baseURL,
		APIKey:    d.cfg.Gateway.APIKey,
		Timeout:   time.Duration(cmp.Or(d.cfg.Client.Timeout, defaultClientTimeoutSeconds)) * time.Second,
		Transport: gateway.Transport(d.cfg.Gateway.TLS),
	}))
	return d.decider
}
