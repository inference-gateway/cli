package kubernetes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	k8sdynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// testWorkspace backs a workspace with a fake dynamic client holding the given
// objects. Without the operator, listing Orchestrators answers not found as a
// cluster without the CRD does.
func testWorkspace(t *testing.T, operator bool, objects ...*k8sunstructured.Unstructured) (*Workspace, *k8sdynamicfake.FakeDynamicClient) {
	t.Helper()
	dynamic := k8sdynamicfake.NewSimpleDynamicClientWithCustomListKinds(k8sruntime.NewScheme(), map[k8sschema.GroupVersionResource]string{orchestratorGVR: "OrchestratorList", gatewayGVR: "GatewayList"})
	for _, object := range objects {
		gvr, _ := k8smeta.UnsafeGuessKindToResource(object.GroupVersionKind())
		if _, err := dynamic.Resource(gvr).Namespace("ns").Create(t.Context(), object, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	dynamic.ClearActions()
	if !operator {
		dynamic.PrependReactor("list", orchestratorResource, func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, k8serrors.NewNotFound(orchestratorGVR.GroupResource(), "")
		})
	}
	return &Workspace{entry: config.WorkspaceEntry{Name: "k8s", Namespace: "ns"}, dynamic: dynamic}, dynamic
}

func testSecret(t *testing.T, secret *corev1.Secret) *k8sunstructured.Unstructured {
	t.Helper()
	object, err := unstructuredSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func storedSecret(t *testing.T, e *Workspace, name string) *corev1.Secret {
	t.Helper()
	object, err := e.secrets().Get(t.Context(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := secretFrom(object)
	if err != nil {
		t.Fatal(err)
	}
	return secret
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
	e, dynamic := testWorkspace(t, false)
	_, err := e.Ensure(t.Context(), agentdomain.Session{ConversationID: "conv-1"})
	if err == nil || !strings.Contains(err.Error(), "the Inference Gateway operator is not installed") {
		t.Fatalf("Ensure without the operator = %v", err)
	}
	for _, action := range dynamic.Actions() {
		if action.GetVerb() != "list" || action.GetResource() != orchestratorGVR {
			t.Fatalf("Ensure without the operator did %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
	if status := e.Probe(t.Context()); status.Reachable || !strings.Contains(status.Error, "operator is not installed") {
		t.Fatalf("Probe without the operator = %+v", status)
	}
}

func TestReachNeverCreatesAMissingOrchestrator(t *testing.T) {
	e, dynamic := testWorkspace(t, true)
	_, err := e.Reach(t.Context(), agentdomain.Session{ConversationID: "conv-1"})
	if err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Fatalf("Reach of a missing Orchestrator = %v", err)
	}
	for _, action := range dynamic.Actions() {
		if action.GetVerb() != "get" && action.GetVerb() != "list" {
			t.Fatalf("Reach of a missing Orchestrator did %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
}

func TestOrchestratorRunsTheDaemonInASandbox(t *testing.T) {
	e, _ := testWorkspace(t, true)
	e.entry.Image = "infer:local"
	e.entry.Storage = "1Gi"
	e.entry.Retention = 24 * time.Hour
	e.entry.Env = map[string]string{"INFER_GATEWAY_URL": "http://gateway:8080", "INFER_AGENT_MODEL": "mock/model"}
	orchestrator, err := e.orchestrator("infer-1", agentdomain.Session{ConversationID: "conv-1"}, nil, nil)
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
	wantNames := "INFER_DAEMON_WORKSPACE INFER_DAEMON_PROJECTS_ROOT INFER_DAEMON_IDLE_EXIT INFER_DAEMON_AWAY_EXIT INFER_AGENT_MODEL INFER_GATEWAY_URL"
	if strings.Join(names, " ") != wantNames {
		t.Fatalf("env = %v, want %s", names, wantNames)
	}
	if orchestrator.GetLabels()[labelConversation] != "conv-1" || orchestrator.GetLabels()[labelWorkspace] != "k8s" {
		t.Fatalf("labels = %v", orchestrator.GetLabels())
	}
	if _, found, _ := k8sunstructured.NestedMap(orchestrator.Object, "spec", "daemon", "binding", "service"); found {
		t.Fatal("spec.daemon.binding.service is set without binding_service")
	}
}

func TestOrchestratorOnAClusterCarriesItsComputeLabel(t *testing.T) {
	e, _ := testWorkspace(t, true)
	session := agentdomain.Session{ConversationID: "conv-1"}
	orchestrator, err := e.orchestrator("infer-1", session, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, labelled := orchestrator.GetLabels()[labelCompute]; labelled {
		t.Fatalf("a workspace with its own connection labels its Orchestrator %v", orchestrator.GetLabels())
	}
	e.entry.Compute = "k3d"
	if orchestrator, err = e.orchestrator("infer-1", session, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := orchestrator.GetLabels(); got[labelCompute] != "k3d" || got[labelWorkspace] != "k8s" || got[labelConversation] != "conv-1" {
		t.Fatalf("labels = %v, want the cluster beside the workspace and conversation", got)
	}
}

func TestOrchestratorCarriesTheBindingServiceMetadata(t *testing.T) {
	e, _ := testWorkspace(t, true)
	e.entry.Image = "infer:local"
	e.entry.BindingService = config.BindingServiceConfig{
		Annotations: map[string]string{"vpn/expose": "true", "vpn/hostname": "{{.Orchestrator}}"},
		Labels:      map[string]string{"session": "{{.Orchestrator}}"},
	}
	orchestrator, err := e.orchestrator("infer-1", agentdomain.Session{ConversationID: "conv-1"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	service, _, _ := k8sunstructured.NestedMap(orchestrator.Object, "spec", "daemon", "binding", "service")
	want := map[string]any{
		"annotations": map[string]any{"vpn/expose": "true", "vpn/hostname": "infer-1"},
		"labels":      map[string]any{"session": "infer-1"},
	}
	if !reflect.DeepEqual(service, want) {
		t.Fatalf("spec.daemon.binding.service = %v, want %v", service, want)
	}
	if orchestrator.DeepCopy() == nil {
		t.Fatal("the Orchestrator is not deep-copyable")
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
			e, _ := testWorkspace(t, true, running)
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
	e, dynamic := testWorkspace(t, true, suspended)
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

func TestCreateReferencesEveryKeyOfTheWorkspaceSecrets(t *testing.T) {
	mail := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "reviewer-mail", Namespace: "ns"}, Data: map[string][]byte{
		"AGENTMAIL_API_KEY": []byte("am_us_reviewer"),
		"AGENTMAIL_INBOX":   []byte("reviewer@agentmail.to"),
	}}
	github := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "reviewer-github", Namespace: "ns"}, Data: map[string][]byte{"GITHUB_TOKEN": []byte("ghp_x")}}

	tests := []struct {
		name    string
		secrets []string
		want    []any
		wantErr string
	}{
		{name: "no secrets adds nothing", secrets: nil, want: []any{}},
		{
			name:    "every key of every Secret in order, never the values",
			secrets: []string{"reviewer-mail", "reviewer-github"},
			want: []any{
				secretEnvVar("AGENTMAIL_API_KEY", "reviewer-mail"),
				secretEnvVar("AGENTMAIL_INBOX", "reviewer-mail"),
				secretEnvVar("GITHUB_TOKEN", "reviewer-github"),
			},
		},
		{name: "a missing Secret fails before anything is created", secrets: []string{"reviewer-mail", "ghost"}, wantErr: "names Secret ghost, which namespace ns does not have"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, dynamic := testWorkspace(t, true, testSecret(t, mail.DeepCopy()), testSecret(t, github.DeepCopy()))
			e.entry.Secrets = tt.secrets

			_, err := e.create(t.Context(), "infer-1", agentdomain.Session{ConversationID: "conv-1"})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("create = %v, want an error containing %q", err, tt.wantErr)
				}
				for _, action := range dynamic.Actions() {
					if action.GetVerb() == "create" {
						t.Fatalf("created %s although a Secret is missing", action.GetResource().Resource)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			stored, err := e.orchestrators().Get(t.Context(), "infer-1", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			env, _, _ := k8sunstructured.NestedSlice(stored.Object, "spec", "env")
			if got := env[len(e.env()):]; !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("secret env = %v, want %v", got, tt.want)
			}
			if strings.Contains(fmt.Sprint(env), "am_us_reviewer") {
				t.Fatal("a Secret value landed in the Orchestrator")
			}
		})
	}
}

func TestOwnSecretMakesTheOrchestratorItsOwner(t *testing.T) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "infer-1" + tlsSecretSuffix, Namespace: "ns"}}
	e, _ := testWorkspace(t, true, testSecret(t, secret))
	if err := e.ownSecret(t.Context(), testOrchestrator("infer-1", map[string]any{}, nil)); err != nil {
		t.Fatal(err)
	}
	owners := storedSecret(t, e, secret.Name).GetOwnerReferences()
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

const testMCPYAML = `enabled: true
servers:
  - name: mailbox
    enabled: true
    url: https://mcp.example.com/mcp
    headers:
      x-api-key: ${MAILBOX_API_KEY}
      Authorization: Bearer ${MAILBOX_TOKEN}
  - name: split
    enabled: false
    scheme: https
    host: split.example.com
    port: 8443
    path: /rpc
  - name: container
    enabled: true
    run: true
    oci: example/mcp:latest
  - name: leaky
    enabled: true
    url: https://leaky.example.com/mcp
    headers:
      x-api-key: plain-secret-value
`

func TestCreateGivesThePodTheNamedMCPServersAsWritten(t *testing.T) {
	t.Setenv("MAILBOX_API_KEY", "expanded-on-this-machine")
	mcpPath := filepath.Join(t.TempDir(), "mcp.yaml")
	if err := os.WriteFile(mcpPath, []byte(testMCPYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		servers []string
		want    map[string]any
		wantErr string
	}{
		{name: "no servers leaves MCP to the image"},
		{
			name:    "every named server with its URL and its header references",
			servers: []string{"mailbox", "split"},
			want: map[string]any{"enabled": true, "hostedServers": []any{
				map[string]any{"name": "mailbox", "url": "https://mcp.example.com/mcp", "headers": map[string]any{
					"x-api-key":     "${MAILBOX_API_KEY}",
					"Authorization": "Bearer ${MAILBOX_TOKEN}",
				}},
				map[string]any{"name": "split", "url": "https://split.example.com:8443/rpc"},
			}},
		},
		{name: "a name mcp.yaml lacks", servers: []string{"mailbox", "ghost"}, wantErr: "names MCP server ghost, which " + mcpPath + " does not have"},
		{name: "a container server", servers: []string{"container"}, wantErr: "names MCP server container, a container on this machine"},
		{name: "a header holding a value", servers: []string{"leaky"}, wantErr: "MCP server leaky gives header x-api-key a value rather than a ${VAR} reference"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, dynamic := testWorkspace(t, true)
			e.mcpPath = mcpPath
			e.entry.MCPServers = tt.servers

			_, err := e.create(t.Context(), "infer-1", agentdomain.Session{ConversationID: "conv-1"})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("create = %v, want an error containing %q", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), "plain-secret-value") {
					t.Fatalf("the error shows the header's value: %v", err)
				}
				for _, action := range dynamic.Actions() {
					if action.GetVerb() == "create" {
						t.Fatalf("created %s although an MCP server cannot reach the pod", action.GetResource().Resource)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			stored, err := e.orchestrators().Get(t.Context(), "infer-1", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			got, _, _ := k8sunstructured.NestedMap(stored.Object, "spec", "mcp")
			if tt.want == nil && got != nil || tt.want != nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("spec.mcp = %v, want %v", got, tt.want)
			}
			if strings.Contains(fmt.Sprint(stored.Object), "expanded-on-this-machine") {
				t.Fatal("a header was expanded on this machine into the Orchestrator")
			}
		})
	}
}

func TestCreateNamesTheOperatorVersionHostedMCPServersNeed(t *testing.T) {
	mcpPath := filepath.Join(t.TempDir(), "mcp.yaml")
	if err := os.WriteFile(mcpPath, []byte(testMCPYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	e, dynamic := testWorkspace(t, true)
	e.mcpPath = mcpPath
	e.entry.MCPServers = []string{"mailbox"}
	dynamic.PrependReactor("create", orchestratorResource, func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, k8serrors.NewBadRequest(`strict decoding error: unknown field "spec.mcp.hostedServers"`)
	})
	_, err := e.create(t.Context(), "infer-1", agentdomain.Session{ConversationID: "conv-1"})
	if err == nil || !strings.Contains(err.Error(), "may predate "+hostedMCPOperatorVersion+", upgrade it") {
		t.Fatalf("create on an older operator = %v", err)
	}
}
