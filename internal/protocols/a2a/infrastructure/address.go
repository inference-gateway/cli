package infrastructure

import (
	"net/url"
	"sync"

	config "github.com/inference-gateway/cli/config"
)

// agentRoute sends requests for an agent's configured origin to the host its
// container actually listens on.
type agentRoute struct {
	origin string
	host   string
}

// agentRoutes holds the local agents the supervisor had to start on another
// host port than agents.yaml names. The configured URL stays the agent's identity.
// ponytail: one process-wide table, as one supervisor runs per process. Pass it
// through NewClient if a process ever runs several.
var agentRoutes = struct {
	sync.RWMutex
	byAgent map[string]agentRoute
}{byAgent: map[string]agentRoute{}}

// RouteAgent sends requests for the named agent, configured at configuredURL,
// to the host of runningAt. Running on the configured origin clears the route.
func RouteAgent(name, configuredURL, runningAt string) {
	running, err := url.Parse(runningAt)
	origin := config.URLOrigin(configuredURL)
	if err != nil || origin == "" || config.URLOrigin(runningAt) == origin {
		UnrouteAgent(name)
		return
	}
	agentRoutes.Lock()
	defer agentRoutes.Unlock()
	agentRoutes.byAgent[name] = agentRoute{origin: origin, host: running.Host}
}

// UnrouteAgent sends requests for the named agent back to its configured URL.
func UnrouteAgent(name string) {
	agentRoutes.Lock()
	defer agentRoutes.Unlock()
	delete(agentRoutes.byAgent, name)
}

// runningURL returns agentURL on the host its agent actually runs on, or
// agentURL itself when the agent runs where it is configured.
func runningURL(agentURL string) string {
	host, routed := routedHost(config.URLOrigin(agentURL))
	if !routed {
		return agentURL
	}
	u, err := url.Parse(agentURL)
	if err != nil {
		return agentURL
	}
	u.Host = host
	return u.String()
}

func routedHost(origin string) (string, bool) {
	if origin == "" {
		return "", false
	}
	agentRoutes.RLock()
	defer agentRoutes.RUnlock()
	for _, route := range agentRoutes.byAgent {
		if route.origin == origin {
			return route.host, true
		}
	}
	return "", false
}
