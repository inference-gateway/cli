package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	k8sdynamicfake "k8s.io/client-go/dynamic/fake"
	k8sclientfake "k8s.io/client-go/kubernetes/fake"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// isolateAuthFile points the auth file at an empty home, so a test never
// picks up the developer's own keys.
func isolateAuthFile(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, config.ConfigDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	return config.AuthFilePath()
}

func TestOrchestratorPointsTheWorkersAtTheGateway(t *testing.T) {
	isolateAuthFile(t)
	t.Setenv("OLLAMA_CLOUD_API_KEY", "ollama-secret")
	tests := []struct {
		name       string
		mode       config.WorkspaceMode
		gatewayEnv []string
		want       map[string]any
	}{
		{"standard links the workspace gateway", "", []string{"OLLAMA_CLOUD_API_KEY"}, map[string]any{"ref": gatewayName("k8s")}},
		{"all-in-one reaches the gateway inside the pod", config.WorkspaceModeAllInOne, []string{"OLLAMA_CLOUD_API_KEY"}, map[string]any{"url": inPodGatewayURL}},
		{"without gateway_env the image or env decides", config.WorkspaceModeStandard, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _, _ := testWorkspace(t, true, nil)
			e.entry.Mode, e.entry.GatewayEnv = tt.mode, tt.gatewayEnv
			orchestrator, err := e.orchestrator("infer-1", agentdomain.Session{ConversationID: "conv-1"})
			if err != nil {
				t.Fatal(err)
			}
			if gateway, _, _ := k8sunstructured.NestedMap(orchestrator.Object, "spec", "gateway"); !maps.Equal(gateway, tt.want) {
				t.Fatalf("spec.gateway = %v, want %v", gateway, tt.want)
			}
			if raw, _ := json.Marshal(orchestrator.Object); strings.Contains(string(raw), "ollama-secret") {
				t.Fatal("the Orchestrator carries the provider key")
			}
		})
	}
}

func TestEnsureGatewayKeepsCredentialsOnTheGateway(t *testing.T) {
	authFile := isolateAuthFile(t)
	e, dynamic, typed := testWorkspace(t, true, nil)
	e.entry.GatewayEnv = []string{"OLLAMA_CLOUD_API_KEY", "OPENAI_API_KEY"}
	first, second := testOrchestrator("infer-1", map[string]any{}, nil), testOrchestrator("infer-2", map[string]any{}, nil)
	t.Setenv("OPENAI_API_KEY", "")

	t.Setenv("OLLAMA_CLOUD_API_KEY", "key-1")
	secret, gateway := ensureGatewayFor(t, e, dynamic, typed, first)
	assertCredentials(t, secret, "key-1")
	assertOwners(t, secret.OwnerReferences, gateway.GetOwnerReferences(), "uid-infer-1")
	if got := providerNames(gateway); !slices.Equal(got, []string{"ollama_cloud", "openai", "credentials"}) {
		t.Fatalf("providers = %v", got)
	}
	ref, _, _ := k8sunstructured.NestedMap(providerEnv(gateway, "ollama_cloud")[0].(map[string]any), "valueFrom", "secretKeyRef")
	if ref["name"] != secret.Name || ref["key"] != "OLLAMA_CLOUD_API_KEY" || ref["optional"] != true {
		t.Fatalf("ollama_cloud secretKeyRef = %v", ref)
	}
	assertRevision(t, secret, gateway, "1")
	if raw, _ := json.Marshal(gateway.Object); strings.Contains(string(raw), "key-1") {
		t.Fatal("the Gateway carries the provider key")
	}

	secret, gateway = ensureGatewayFor(t, e, dynamic, typed, second)
	assertCredentials(t, secret, "key-1")
	assertOwners(t, secret.OwnerReferences, gateway.GetOwnerReferences(), "uid-infer-1", "uid-infer-2")
	assertRevision(t, secret, gateway, "1")

	t.Setenv("OLLAMA_CLOUD_API_KEY", "key-2")
	secret, gateway = ensureGatewayFor(t, e, dynamic, typed, first)
	assertCredentials(t, secret, "key-2")
	assertRevision(t, secret, gateway, "2")

	t.Setenv("OLLAMA_CLOUD_API_KEY", "")
	secret, gateway = ensureGatewayFor(t, e, dynamic, typed, first)
	assertCredentials(t, secret, "key-2")
	assertRevision(t, secret, gateway, "2")

	if err := os.WriteFile(authFile, []byte("OLLAMA_CLOUD_API_KEY: key-from-auth\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret, gateway = ensureGatewayFor(t, e, dynamic, typed, first)
	assertCredentials(t, secret, "key-from-auth")
	assertRevision(t, secret, gateway, "3")
}

func ensureGatewayFor(t *testing.T, e *Workspace, dynamic *k8sdynamicfake.FakeDynamicClient, typed *k8sclientfake.Clientset, orchestrator *k8sunstructured.Unstructured) (*corev1.Secret, *k8sunstructured.Unstructured) {
	t.Helper()
	if err := e.ensureGateway(t.Context(), orchestrator); err != nil {
		t.Fatal(err)
	}
	secret, err := typed.CoreV1().Secrets("ns").Get(t.Context(), gatewayName("k8s")+credentialsSuffix, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := dynamic.Resource(gatewayGVR).Namespace("ns").Get(t.Context(), gatewayName("k8s"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return secret, gateway
}

// assertRevision checks the Gateway rolls only when the credentials change,
// never for a new owner.
func assertRevision(t *testing.T, secret *corev1.Secret, gateway *k8sunstructured.Unstructured, want string) {
	t.Helper()
	if got := revisionOf(gateway); got != want || secret.Annotations[revisionKey] != want {
		t.Fatalf("revision = %q on the Gateway and %q on the Secret, want %q", got, secret.Annotations[revisionKey], want)
	}
}

func assertCredentials(t *testing.T, secret *corev1.Secret, key string) {
	t.Helper()
	if want := map[string][]byte{"OLLAMA_CLOUD_API_KEY": []byte(key)}; !maps.EqualFunc(secret.Data, want, bytes.Equal) {
		t.Fatalf("Secret data = %q, want only OLLAMA_CLOUD_API_KEY=%s", secret.Data, key)
	}
}

func assertOwners(t *testing.T, secretOwners, gatewayOwners []metav1.OwnerReference, uids ...string) {
	t.Helper()
	for _, owners := range [][]metav1.OwnerReference{secretOwners, gatewayOwners} {
		var got []string
		for _, owner := range owners {
			got = append(got, string(owner.UID))
		}
		if !slices.Equal(got, uids) {
			t.Fatalf("owners = %v, want %v", got, uids)
		}
	}
}

func providerNames(gateway *k8sunstructured.Unstructured) []string {
	providers, _, _ := k8sunstructured.NestedSlice(gateway.Object, "spec", "providers")
	names := make([]string, 0, len(providers))
	for _, provider := range providers {
		names = append(names, provider.(map[string]any)["name"].(string))
	}
	return names
}

func providerEnv(gateway *k8sunstructured.Unstructured, name string) []any {
	providers, _, _ := k8sunstructured.NestedSlice(gateway.Object, "spec", "providers")
	for _, provider := range providers {
		if provider.(map[string]any)["name"] == name {
			env, _, _ := k8sunstructured.NestedSlice(provider.(map[string]any), "env")
			return env
		}
	}
	return nil
}

func revisionOf(gateway *k8sunstructured.Unstructured) string {
	value, _ := providerEnv(gateway, "credentials")[0].(map[string]any)["value"].(string)
	return value
}

func TestAwaitGateway(t *testing.T) {
	tests := []struct {
		name       string
		deployment *appsv1.Deployment
		wantErr    string
	}{
		{"a pod serves", gatewayDeployment(1), ""},
		{"no pod serves yet", gatewayDeployment(0), "has no available pod in time"},
		{"the operator skipped the namespace", nil, "WATCH_NAMESPACE_SELECTOR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []k8sruntime.Object
			if tt.deployment != nil {
				objects = append(objects, tt.deployment)
			}
			e, _, _ := testWorkspace(t, true, nil, objects...)
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			err := e.awaitGateway(ctx)
			if (err == nil) != (tt.wantErr == "") || (err != nil && !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("awaitGateway = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestReachWaitsForAParkedGateway(t *testing.T) {
	name := orchestratorName("k8s", "conv-1")
	running := testOrchestrator(name, map[string]any{"sandbox": map[string]any{"operatingMode": modeRunning}}, readyStatus(2, "True", "SandboxReady"))
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name + tlsSecretSuffix, Namespace: "ns"}, Data: map[string][]byte{secretTokenKey: []byte("token")}}
	e, _, _ := testWorkspace(t, true, []*k8sunstructured.Unstructured{running}, secret, gatewayDeployment(0))
	e.entry.GatewayEnv = []string{"OLLAMA_CLOUD_API_KEY"}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := e.Reach(ctx, agentdomain.Session{ConversationID: "conv-1"}); err == nil || !strings.Contains(err.Error(), "has no available pod in time") {
		t.Fatalf("Reach while the gateway is parked = %v, want it to wait for the gateway", err)
	}
}

func gatewayDeployment(available int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayName("k8s"), Namespace: "ns"},
		Status:     appsv1.DeploymentStatus{AvailableReplicas: available},
	}
}
