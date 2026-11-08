package kubernetes

import (
	"context"
	"strings"
	"testing"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	k8sdynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	config "github.com/inference-gateway/cli/config"
)

func fakeCluster(t *testing.T, operator bool, labels ...map[string]string) *k8sdynamicfake.FakeDynamicClient {
	t.Helper()
	dynamic := k8sdynamicfake.NewSimpleDynamicClientWithCustomListKinds(k8sruntime.NewScheme(), map[k8sschema.GroupVersionResource]string{OrchestratorGVR: "OrchestratorList"})
	for i, label := range labels {
		orchestrator := &k8sunstructured.Unstructured{Object: map[string]any{"apiVersion": OrchestratorGroup + "/" + OrchestratorVersion, "kind": "Orchestrator"}}
		orchestrator.SetName("infer-" + string(rune('a'+i)))
		orchestrator.SetNamespace("agents")
		orchestrator.SetLabels(label)
		if _, err := dynamic.Resource(OrchestratorGVR).Namespace("agents").Create(t.Context(), orchestrator, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if !operator {
		dynamic.PrependReactor("list", "orchestrators", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, k8serrors.NewNotFound(OrchestratorGVR.GroupResource(), "")
		})
	}
	return dynamic
}

func version(context.Context) (string, error) { return "v1.31.0", nil }

func TestStatusCountsTheSessionsOfEveryWorkspaceOnTheCluster(t *testing.T) {
	cluster := config.ClusterEntry{Name: "k3d", Namespace: "agents"}
	dynamic := fakeCluster(t, true,
		map[string]string{LabelCompute: "k3d", "inference-gateway.com/workspace": "reviewer"},
		map[string]string{LabelCompute: "k3d", "inference-gateway.com/workspace": "releaser"},
		map[string]string{"inference-gateway.com/workspace": "legacy"},
	)

	got := status(t.Context(), cluster, dynamic, version)

	if !got.Reachable || got.Sessions != 2 || got.KubernetesVersion != "v1.31.0" || got.Error != "" {
		t.Fatalf("status = %+v, want reachable with the two sessions labelled for the cluster", got)
	}
}

func TestStatusWithoutTheOperatorSaysWhatToInstall(t *testing.T) {
	got := status(t.Context(), config.ClusterEntry{Name: "k3d", Namespace: "agents"}, fakeCluster(t, false), version)

	if got.Reachable || !strings.Contains(got.Error, "the Inference Gateway operator is not installed in cluster k3d") || !strings.Contains(got.Error, MinOperatorVersion) {
		t.Fatalf("status = %+v, want the missing operator named with its minimum version", got)
	}
}
