package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	k8sdynamic "k8s.io/client-go/dynamic"
	k8srest "k8s.io/client-go/rest"
)

// The environment a pod daemon learns its own Sandbox from.
const (
	EnvSandboxName      = "INFER_DAEMON_KUBERNETES_SANDBOX"
	EnvSandboxNamespace = "INFER_DAEMON_KUBERNETES_NAMESPACE"
	EnvSandboxRetention = "INFER_DAEMON_KUBERNETES_RETENTION"
)

// annotationAgentsDone records on a Sandbox whether every agent was done when
// its pod daemon last suspended it, for a later pod that ran nothing itself.
const annotationAgentsDone = "inference-gateway.com/agents-done"

// SelfSuspend returns the function a pod daemon calls before it idles out: it
// patches its own Sandbox to Suspended through the in-cluster API, so the
// controller parks the pod and keeps the volumes for the next teleport. done
// says whether every agent finished, and known whether this pod ran any.
func SelfSuspend(sandbox, namespace string, retention time.Duration) (func(ctx context.Context, done, known bool) error, error) {
	restConfig, err := k8srest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("loading the in-cluster config: %w", err)
	}
	client, err := k8sdynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	sandboxes := client.Resource(sandboxGVR).Namespace(namespace)
	return func(ctx context.Context, done, known bool) error {
		if !known {
			current, err := sandboxes.Get(ctx, sandbox, metav1.GetOptions{})
			done = err == nil && current.GetAnnotations()[annotationAgentsDone] == "true"
		}
		patch, _ := json.Marshal(suspendPatch(done, retention, time.Now()))
		if _, err := sandboxes.Patch(ctx, sandbox, k8stypes.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("suspending Sandbox %s: %w", sandbox, err)
		}
		return nil
	}, nil
}

// suspendPatch parks a Sandbox and records whether its agents are done. Only
// then does a retention have the agent-sandbox controller delete the Sandbox,
// its volumes included, once it sat suspended that long.
func suspendPatch(done bool, retention time.Duration, now time.Time) map[string]any {
	spec := map[string]any{"operatingMode": "Suspended"}
	if done && retention > 0 {
		spec["shutdownTime"] = now.Add(retention).UTC().Format(time.RFC3339)
		spec["shutdownPolicy"] = "Delete"
	}
	return map[string]any{
		"metadata": map[string]any{"annotations": map[string]any{annotationAgentsDone: strconv.FormatBool(done)}},
		"spec":     spec,
	}
}
