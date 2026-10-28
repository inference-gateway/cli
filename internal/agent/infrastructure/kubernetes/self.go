package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	k8sdynamic "k8s.io/client-go/dynamic"
	k8srest "k8s.io/client-go/rest"
)

// The environment a pod daemon learns its own Sandbox from.
const (
	EnvSandboxName      = "INFER_DAEMON_KUBERNETES_SANDBOX"
	EnvSandboxNamespace = "INFER_DAEMON_KUBERNETES_NAMESPACE"
)

// SelfSuspend returns the function a pod daemon calls before it idles out: it
// patches its own Sandbox to Suspended through the in-cluster API, so the
// controller parks the pod and keeps the volumes for the next transfer.
func SelfSuspend(sandbox, namespace string) (func(ctx context.Context) error, error) {
	restConfig, err := k8srest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("loading the in-cluster config: %w", err)
	}
	client, err := k8sdynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"operatingMode": "Suspended"}})
		_, err := client.Resource(sandboxGVR).Namespace(namespace).Patch(ctx, sandbox, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			return fmt.Errorf("suspending Sandbox %s: %w", sandbox, err)
		}
		return nil
	}, nil
}
