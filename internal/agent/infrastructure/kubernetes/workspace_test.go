package kubernetes

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	k8sdynamicfake "k8s.io/client-go/dynamic/fake"
	k8sclientfake "k8s.io/client-go/kubernetes/fake"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func testWorkspace(t *testing.T, sandboxes []*k8sunstructured.Unstructured, secrets ...k8sruntime.Object) (*Workspace, *k8sdynamicfake.FakeDynamicClient, *k8sclientfake.Clientset) {
	t.Helper()
	dynamic := k8sdynamicfake.NewSimpleDynamicClientWithCustomListKinds(k8sruntime.NewScheme(), map[k8sschema.GroupVersionResource]string{sandboxGVR: "SandboxList"})
	for _, sandbox := range sandboxes {
		if _, err := dynamic.Resource(sandboxGVR).Namespace("ns").Create(t.Context(), sandbox, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	dynamic.ClearActions()
	typed := k8sclientfake.NewClientset(secrets...)
	return &Workspace{entry: config.WorkspaceEntry{Name: "k8s", Namespace: "ns"}, dynamic: dynamic, typed: typed}, dynamic, typed
}

func testSandbox(name string, spec map[string]any) *k8sunstructured.Unstructured {
	sandbox := &k8sunstructured.Unstructured{Object: map[string]any{
		"apiVersion": sandboxGroup + "/" + sandboxVersion,
		"kind":       sandboxKind,
		"metadata":   map[string]any{"name": name, "namespace": "ns"},
		"spec":       spec,
	}}
	sandbox.SetUID(k8stypes.UID("uid-" + name))
	return sandbox
}

func TestSuspendPatchStartsTheRetentionOnlyWhenEveryAgentIsDone(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name           string
		done           bool
		retention      time.Duration
		wantShutdown   string
		wantAnnotation string
	}{
		{"done with a retention", true, 48 * time.Hour, "2026-10-30T12:00:00Z", "true"},
		{"done without a retention", true, 0, "", "true"},
		{"agent not done", false, 48 * time.Hour, "", "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patch := suspendPatch(tt.done, tt.retention, now)
			spec, _ := patch["spec"].(map[string]any)
			if spec["operatingMode"] != "Suspended" {
				t.Fatalf("operatingMode = %v", spec["operatingMode"])
			}
			shutdown, _ := spec["shutdownTime"].(string)
			if shutdown != tt.wantShutdown {
				t.Fatalf("shutdownTime = %q, want %q", shutdown, tt.wantShutdown)
			}
			if policy, set := spec["shutdownPolicy"]; set != (tt.wantShutdown != "") || (set && policy != "Delete") {
				t.Fatalf("shutdownPolicy = %v", policy)
			}
			annotations := patch["metadata"].(map[string]any)["annotations"].(map[string]any)
			if annotations[annotationAgentsDone] != tt.wantAnnotation {
				t.Fatalf("%s = %v, want %s", annotationAgentsDone, annotations[annotationAgentsDone], tt.wantAnnotation)
			}
		})
	}
}

func TestReachNeverCreatesAMissingSandbox(t *testing.T) {
	e, dynamic, typed := testWorkspace(t, nil)
	_, err := e.Reach(t.Context(), agentdomain.Session{ConversationID: "conv-1"})
	if err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Fatalf("Reach of a missing Sandbox = %v", err)
	}
	for _, action := range append(dynamic.Actions(), typed.Actions()...) {
		if action.GetVerb() != "get" {
			t.Fatalf("Reach of a missing Sandbox did %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
}

func TestResumeClearsTheExpiryASuspensionSet(t *testing.T) {
	suspended := testSandbox("infer-1", map[string]any{"operatingMode": "Suspended", "shutdownTime": "2026-10-30T12:00:00Z", "shutdownPolicy": "Delete"})
	e, dynamic, _ := testWorkspace(t, []*k8sunstructured.Unstructured{suspended})
	if err := e.resume(t.Context(), suspended); err != nil {
		t.Fatal(err)
	}
	resumed, err := dynamic.Resource(sandboxGVR).Namespace("ns").Get(t.Context(), "infer-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec, _, _ := k8sunstructured.NestedMap(resumed.Object, "spec")
	if spec["operatingMode"] != "Running" {
		t.Fatalf("operatingMode = %v", spec["operatingMode"])
	}
	if _, set := spec["shutdownTime"]; set {
		t.Fatalf("a resumed Sandbox kept its expiry: %v", spec)
	}
	if _, set := spec["shutdownPolicy"]; set {
		t.Fatalf("a resumed Sandbox kept its shutdown policy: %v", spec)
	}
}

func TestOwnSecretMakesTheSandboxItsOwner(t *testing.T) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "infer-1" + tlsSecretSuffix, Namespace: "ns"}}
	e, _, typed := testWorkspace(t, nil, secret)
	if err := e.ownSecret(t.Context(), testSandbox("infer-1", map[string]any{})); err != nil {
		t.Fatal(err)
	}
	owned, err := typed.CoreV1().Secrets("ns").Get(t.Context(), secret.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	owners := owned.GetOwnerReferences()
	if len(owners) != 1 || owners[0].Kind != sandboxKind || owners[0].Name != "infer-1" || owners[0].UID != "uid-infer-1" {
		t.Fatalf("ownerReferences = %+v", owners)
	}
}
