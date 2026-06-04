package services

import (
	"context"
	"os/exec"
	"strings"

	logger "github.com/inference-gateway/cli/internal/logger"
)

// addressPoolExhaustedMsg is the daemon error emitted when Docker/Podman runs
// out of predefined IPAM address pools — the signature of accumulated leaked
// networks from sessions that never cleaned up.
const addressPoolExhaustedMsg = "all predefined address pools have been fully subnetted"

// isAddressPoolExhausted reports whether a network-create failure was caused by
// the runtime running out of predefined IPAM address pools.
func isAddressPoolExhausted(msg string) bool {
	return strings.Contains(msg, addressPoolExhaustedMsg)
}

// networksToPrune filters network names down to the infer-managed networks that
// are safe to remove: it drops blanks, the current (in-use) network, and any
// name that is not one of ours. Removing a network that is still in use is
// refused by the runtime, so the result is inherently best-effort.
func networksToPrune(all []string, current string) []string {
	out := make([]string, 0, len(all))
	for _, name := range all {
		name = strings.TrimSpace(name)
		if name == "" || name == current {
			continue
		}
		if !strings.HasPrefix(name, InferNetworkPrefix) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// interpretNetworkRm classifies the output of a failed "network rm". gone means
// the network no longer exists (treat as success); inUse means it is still
// attached to a container/session and should be left in place.
func interpretNetworkRm(output string) (gone bool, inUse bool) {
	switch {
	case strings.Contains(output, "not found"), strings.Contains(output, "No such network"):
		return true, false
	case strings.Contains(output, "in use"), strings.Contains(output, "has active endpoints"):
		return false, true
	default:
		return false, false
	}
}

// pruneNetworks best-effort removes leaked infer-managed networks left behind by
// prior sessions so a fresh network can be created after address-pool
// exhaustion. The current network is preserved; networks still in use are
// refused by the runtime and silently skipped. bin is the container CLI
// ("docker" or "podman").
func pruneNetworks(ctx context.Context, bin, current string) {
	out, err := exec.CommandContext(ctx, bin, "network", "ls", "--filter", "name="+InferNetworkPrefix, "--format", "{{.Name}}").Output()
	if err != nil {
		logger.Debug("Failed to list networks for pruning", "runtime", bin, "error", err)
		return
	}

	names := networksToPrune(strings.Split(string(out), "\n"), current)
	if len(names) == 0 {
		return
	}

	logger.Info("Pruning leaked container networks", "runtime", bin, "count", len(names))
	args := append([]string{"network", "rm"}, names...)
	if err := exec.CommandContext(ctx, bin, args...).Run(); err != nil {
		logger.Debug("Some leaked networks could not be removed (likely in use)", "runtime", bin, "error", err)
	}
}
