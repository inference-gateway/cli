package kubernetes

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	k8sfakediscovery "k8s.io/client-go/discovery/fake"
	k8sdynamicfake "k8s.io/client-go/dynamic/fake"
	k8sclientfake "k8s.io/client-go/kubernetes/fake"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func testWorkspace(t *testing.T, operator bool, orchestrators []*k8sunstructured.Unstructured, secrets ...k8sruntime.Object) (*Workspace, *k8sdynamicfake.FakeDynamicClient, *k8sclientfake.Clientset) {
	t.Helper()
	dynamic := k8sdynamicfake.NewSimpleDynamicClientWithCustomListKinds(k8sruntime.NewScheme(), map[k8sschema.GroupVersionResource]string{orchestratorGVR: "OrchestratorList"})
	for _, orchestrator := range orchestrators {
		if _, err := dynamic.Resource(orchestratorGVR).Namespace("ns").Create(t.Context(), orchestrator, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	dynamic.ClearActions()
	typed := k8sclientfake.NewClientset(secrets...)
	if operator {
		typed.Discovery().(*k8sfakediscovery.FakeDiscovery).Resources = []*metav1.APIResourceList{{
			GroupVersion: orchestratorGroup + "/" + orchestratorVersion,
			APIResources: []metav1.APIResource{{Name: orchestratorResource, Kind: orchestratorKind, Namespaced: true}},
		}}
	}
	return &Workspace{entry: config.WorkspaceEntry{Name: "k8s", Namespace: "ns"}, dynamic: dynamic, typed: typed}, dynamic, typed
}

func testOrchestrator(name string, spec, status map[string]any) *k8sunstructured.Unstructured {
	orchestrator := &k8sunstructured.Unstructured{Object: map[string]any{
		"apiVersion": orchestratorGroup + "/" + orchestratorVersion,
		"kind":       orchestratorKind,
		"metadata":   map[string]any{"name": name, "namespace": "ns"},
		"spec":       spec,
		"status":     status,
	}}
	orchestrator.SetUID(k8stypes.UID("uid-" + name))
	orchestrator.SetGeneration(2)
	return orchestrator
}

func readyStatus(observedGeneration int64, status, reason string) map[string]any {
	return map[string]any{
		"podName": "infer-1-pod",
		"conditions": []any{map[string]any{
			"type": "Ready", "status": status, "reason": reason, "message": "the operator says " + reason, "observedGeneration": observedGeneration,
		}},
	}
}

func TestEnsureAbortsWhenTheOperatorIsNotInstalled(t *testing.T) {
	e, dynamic, typed := testWorkspace(t, false, nil)
	_, err := e.Ensure(t.Context(), agentdomain.Session{ConversationID: "conv-1"})
	if err == nil || !strings.Contains(err.Error(), "the Inference Gateway operator is not installed") {
		t.Fatalf("Ensure without the operator = %v", err)
	}
	if actions := dynamic.Actions(); len(actions) != 0 {
		t.Fatalf("Ensure without the operator touched Orchestrators: %v", actions)
	}
	for _, action := range typed.Actions() {
		if action.GetVerb() != "get" || action.GetResource().Resource == "secrets" {
			t.Fatalf("Ensure without the operator did %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
	if status := e.Probe(t.Context()); status.Reachable || !strings.Contains(status.Error, "operator is not installed") {
		t.Fatalf("Probe without the operator = %+v", status)
	}
}

func TestReachNeverCreatesAMissingOrchestrator(t *testing.T) {
	e, dynamic, typed := testWorkspace(t, true, nil)
	_, err := e.Reach(t.Context(), agentdomain.Session{ConversationID: "conv-1"})
	if err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Fatalf("Reach of a missing Orchestrator = %v", err)
	}
	for _, action := range append(dynamic.Actions(), typed.Actions()...) {
		if action.GetVerb() != "get" {
			t.Fatalf("Reach of a missing Orchestrator did %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
}

func TestOrchestratorRunsTheDaemonInASandbox(t *testing.T) {
	e, _, _ := testWorkspace(t, true, nil)
	e.entry.Image = "infer:local"
	e.entry.Storage = "1Gi"
	e.entry.Retention = 24 * time.Hour
	e.entry.Env = map[string]string{"INFER_GATEWAY_URL": "http://gateway:8080", "INFER_AGENT_MODEL": "mock/model"}
	orchestrator, err := e.orchestrator("infer-1", agentdomain.Session{ConversationID: "conv-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"image":                         "infer:local",
		"runtime":                       "Sandbox",
		"sandbox.operatingMode":         modeRunning,
		"sandbox.storage":               "1Gi",
		"sandbox.retention":             "24h0m0s",
		"daemon.binding.port":           int64(bindingPort),
		"daemon.binding.tokenSecretRef": map[string]any{"name": "infer-1-tls", "key": secretTokenKey},
		"daemon.binding.tls":            map[string]any{"secretName": "infer-1-tls", "clientAuth": true},
		"tools.enabled":                 true,
		"a2a.enabled":                   true,
	}
	for path, value := range want {
		got, found, _ := k8sunstructured.NestedFieldNoCopy(orchestrator.Object, append([]string{"spec"}, strings.Split(path, ".")...)...)
		if !found || !reflect.DeepEqual(got, value) {
			t.Errorf("spec.%s = %v, want %v", path, got, value)
		}
	}
	env, _, _ := k8sunstructured.NestedSlice(orchestrator.Object, "spec", "env")
	var names []string
	for _, raw := range env {
		names = append(names, raw.(map[string]any)["name"].(string))
	}
	wantNames := "INFER_DAEMON_WORKSPACE INFER_DAEMON_PROJECTS_ROOT INFER_DAEMON_IDLE_EXIT INFER_AGENT_MODEL INFER_GATEWAY_URL"
	if strings.Join(names, " ") != wantNames {
		t.Fatalf("env = %v, want %s", names, wantNames)
	}
	if orchestrator.GetLabels()[labelConversation] != "conv-1" || orchestrator.GetLabels()[labelWorkspace] != "k8s" {
		t.Fatalf("labels = %v", orchestrator.GetLabels())
	}
}

func TestAwaitReady(t *testing.T) {
	tests := []struct {
		name    string
		status  map[string]any
		wantPod string
		wantErr string
	}{
		{"ready for the current spec", readyStatus(2, "True", "SandboxReady"), "infer-1-pod", ""},
		{"ready for an older spec", readyStatus(1, "True", "SandboxReady"), "", "did not become ready in time"},
		{"agent-sandbox missing", readyStatus(2, "False", "SandboxCRDMissing"), "", "cannot run: the operator says SandboxCRDMissing"},
		{"nothing to run", readyStatus(2, "False", "InvalidSpec"), "", "cannot run"},
		{"still starting", readyStatus(2, "False", "SandboxNotReady"), "", "did not become ready in time: the operator says SandboxNotReady"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			running := testOrchestrator("infer-1", map[string]any{"sandbox": map[string]any{"operatingMode": modeRunning}}, tt.status)
			e, _, _ := testWorkspace(t, true, []*k8sunstructured.Unstructured{running})
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			pod, err := e.awaitReady(ctx, "infer-1")
			if pod != tt.wantPod || (err == nil) != (tt.wantErr == "") || (err != nil && !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("awaitReady = %q, %v, want %q, %q", pod, err, tt.wantPod, tt.wantErr)
			}
		})
	}
}

func TestAwaitReadyResumesASuspendedOrchestrator(t *testing.T) {
	suspended := testOrchestrator("infer-1", map[string]any{"sandbox": map[string]any{"operatingMode": modeSuspended, "retention": "24h"}}, readyStatus(2, "False", "SandboxSuspended"))
	e, dynamic, _ := testWorkspace(t, true, []*k8sunstructured.Unstructured{suspended})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := e.awaitReady(ctx, "infer-1"); err == nil {
		t.Fatal("awaitReady of an Orchestrator the operator never resumed succeeded")
	}
	resumed, err := dynamic.Resource(orchestratorGVR).Namespace("ns").Get(t.Context(), "infer-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sandbox, _, _ := k8sunstructured.NestedMap(resumed.Object, "spec", "sandbox")
	if sandbox["operatingMode"] != modeRunning || sandbox["retention"] != "24h" {
		t.Fatalf("spec.sandbox = %v, want Running with its retention kept", sandbox)
	}
}

func TestOwnSecretMakesTheOrchestratorItsOwner(t *testing.T) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "infer-1" + tlsSecretSuffix, Namespace: "ns"}}
	e, _, typed := testWorkspace(t, true, nil, secret)
	if err := e.ownSecret(t.Context(), testOrchestrator("infer-1", map[string]any{}, nil)); err != nil {
		t.Fatal(err)
	}
	owned, err := typed.CoreV1().Secrets("ns").Get(t.Context(), secret.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	owners := owned.GetOwnerReferences()
	if len(owners) != 1 || owners[0].Kind != orchestratorKind || owners[0].Name != "infer-1" || owners[0].UID != "uid-infer-1" {
		t.Fatalf("ownerReferences = %+v", owners)
	}
}

func TestReportIdleWritesTheAgentsDoneMessage(t *testing.T) {
	tests := []struct {
		name string
		done bool
		want string
	}{
		{"every agent done", true, "agents-done=true"},
		{"an agent not done", false, "agents-done=false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "termination-log")
			if err := os.WriteFile(path, []byte("agents-done=something-longer"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := writeTerminationMessage(path, tt.done); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(path); string(got) != tt.want {
				t.Fatalf("termination message = %q, want %q", got, tt.want)
			}
		})
	}
	t.Run("outside a pod", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "termination-log")
		if err := writeTerminationMessage(path, true); err != nil {
			t.Fatalf("writeTerminationMessage without a termination log = %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("writeTerminationMessage created %s", path)
		}
	})
}
