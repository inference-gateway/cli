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
	k8sfield "k8s.io/apimachinery/pkg/util/validation/field"
	k8sdynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
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

func TestOrchestratorBootsWithTheUserDataOfTheSessionSecret(t *testing.T) {
	e, _ := testWorkspace(t, true)
	session := agentdomain.Session{ConversationID: "conv-1"}
	orchestrator, err := e.orchestrator("infer-1", session, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, _ := k8sunstructured.NestedMap(orchestrator.Object, "spec", "userData"); found {
		t.Fatal("spec.userData is set without user_data")
	}

	e.entry.UserData = "#!/bin/sh\napt-get install -y firefox-esr\n"
	if orchestrator, err = e.orchestrator("infer-1", session, nil, nil); err != nil {
		t.Fatal(err)
	}
	secretRef, _, _ := k8sunstructured.NestedMap(orchestrator.Object, "spec", "userData", "secretRef")
	if want := map[string]any{"name": "infer-1-tls", "key": "userdata"}; !reflect.DeepEqual(secretRef, want) {
		t.Fatalf("spec.userData.secretRef = %v, want %v", secretRef, want)
	}
	if strings.Contains(fmt.Sprint(orchestrator.Object), "firefox-esr") {
		t.Fatal("the user-data landed in the Orchestrator instead of its Secret")
	}
}

func TestOrchestratorMountsTheWorkspaceVolumes(t *testing.T) {
	e, _ := testWorkspace(t, true)
	session := agentdomain.Session{ConversationID: "conv-1"}
	orchestrator, err := e.orchestrator("infer-1", session, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, _ := k8sunstructured.NestedSlice(orchestrator.Object, "spec", "sandbox", "volumes"); found {
		t.Fatal("spec.sandbox.volumes is set without volumes")
	}

	e.entry.Volumes = []config.WorkspaceVolume{
		{Name: "resolve", Claim: "davinci-resolve", MountPath: "/opt/resolve"},
		{Name: "footage", Claim: "raw-footage", MountPath: "/mnt/footage", ReadOnly: true},
	}
	if orchestrator, err = e.orchestrator("infer-1", session, nil, nil); err != nil {
		t.Fatal(err)
	}
	volumes, _, _ := k8sunstructured.NestedSlice(orchestrator.Object, "spec", "sandbox", "volumes")
	want := []any{
		map[string]any{"name": "resolve", "claimName": "davinci-resolve", "mountPath": "/opt/resolve"},
		map[string]any{"name": "footage", "claimName": "raw-footage", "mountPath": "/mnt/footage", "readOnly": true},
	}
	if !reflect.DeepEqual(volumes, want) {
		t.Fatalf("spec.sandbox.volumes = %v, want %v", volumes, want)
	}
}

func TestOrchestratorCarriesTheWorkspaceResources(t *testing.T) {
	tests := []struct {
		name      string
		resources config.WorkspaceResources
		want      map[string]any
		wantErr   string
	}{
		{name: "none leaves the pod to the cluster's defaults"},
		{
			name: "requests and limits",
			resources: config.WorkspaceResources{
				Requests: map[string]string{"cpu": "2", "memory": "4Gi"},
				Limits:   map[string]string{"cpu": "8", "memory": "12Gi", "devic.es/dri": "1"},
			},
			want: map[string]any{
				"requests": map[string]any{"cpu": "2", "memory": "4Gi"},
				"limits":   map[string]any{"cpu": "8", "memory": "12Gi", "devic.es/dri": "1"},
			},
		},
		{
			name:      "limits alone",
			resources: config.WorkspaceResources{Limits: map[string]string{"memory": "12Gi"}},
			want:      map[string]any{"limits": map[string]any{"memory": "12Gi"}},
		},
		{
			name:      "a request equal to its limit in another unit",
			resources: config.WorkspaceResources{Requests: map[string]string{"cpu": "2000m"}, Limits: map[string]string{"cpu": "2"}},
			want:      map[string]any{"requests": map[string]any{"cpu": "2000m"}, "limits": map[string]any{"cpu": "2"}},
		},
		{
			name:      "a value that is no quantity",
			resources: config.WorkspaceResources{Limits: map[string]string{"memory": "lots"}},
			wantErr:   `workspace k8s: resources.limits.memory "lots" is not a quantity`,
		},
		{
			name:      "a request above its limit",
			resources: config.WorkspaceResources{Requests: map[string]string{"memory": "16Gi"}, Limits: map[string]string{"memory": "12Gi"}},
			wantErr:   `workspace k8s: resources.requests.memory "16Gi" is above resources.limits.memory "12Gi"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _ := testWorkspace(t, true)
			e.entry.Resources = tt.resources
			orchestrator, err := e.orchestrator("infer-1", agentdomain.Session{ConversationID: "conv-1"}, nil, nil)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("orchestrator() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			resources, found, _ := k8sunstructured.NestedMap(orchestrator.Object, "spec", "resources")
			if found != (tt.want != nil) || !reflect.DeepEqual(resources, tt.want) {
				t.Fatalf("spec.resources = %v (found %v), want %v", resources, found, tt.want)
			}
		})
	}
}

func TestCreateNamesTheOperatorVersionVolumesNeed(t *testing.T) {
	e, dynamic := testWorkspace(t, true)
	e.entry.Volumes = []config.WorkspaceVolume{{Name: "footage", Claim: "raw-footage", MountPath: "/mnt/footage"}}
	dynamic.PrependReactor("create", orchestratorResource, func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, k8serrors.NewBadRequest(`strict decoding error: unknown field "spec.sandbox.volumes"`)
	})
	_, err := e.create(t.Context(), "infer-1", agentdomain.Session{ConversationID: "conv-1"})
	if err == nil || !strings.Contains(err.Error(), "may predate "+volumesOperatorVersion+", upgrade it") {
		t.Fatalf("create on an older operator = %v", err)
	}
}

func TestCreateDoesNotBlameTheOperatorForAValueItsSchemaRejects(t *testing.T) {
	e, dynamic := testWorkspace(t, true)
	e.entry.Volumes = []config.WorkspaceVolume{{Name: "Footage", Claim: "raw-footage", MountPath: "/mnt/footage"}}
	dynamic.PrependReactor("create", orchestratorResource, func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		invalid := k8sfield.Invalid(k8sfield.NewPath("spec", "sandbox", "volumes").Index(0).Child("name"), "Footage", "should match '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'")
		return true, nil, k8serrors.NewInvalid(orchestratorGVR.GroupVersion().WithKind(orchestratorKind).GroupKind(), "infer-1", k8sfield.ErrorList{invalid})
	})
	_, err := e.create(t.Context(), "infer-1", agentdomain.Session{ConversationID: "conv-1"})
	if err == nil || strings.Contains(err.Error(), "predate") || !strings.Contains(err.Error(), "should match") {
		t.Fatalf("create with an uppercase volume name = %v", err)
	}
}

func TestEnsureSecretHoldsTheUserDataThePodBootsWith(t *testing.T) {
	e, _ := testWorkspace(t, true)
	pki, err := agentinfra.LoadOrCreatePKI(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.pki = pki
	e.entry.UserData = "#!/bin/sh\napt-get install -y firefox-esr\n"
	if _, err := e.ensureSecret(t.Context(), "infer-1"); err != nil {
		t.Fatal(err)
	}
	stored := storedSecret(t, e, "infer-1"+tlsSecretSuffix)
	if got := string(stored.Data[secretUserDataKey]); got != e.entry.UserData {
		t.Fatalf("Secret key %s = %q, want the workspace's user_data", secretUserDataKey, got)
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
  - name: filtered
    enabled: true
    url: https://filtered.example.com/mcp
    include_tools:
      - search
    exclude_tools:
      - delete
    timeout: 10
  - name: unbounded
    enabled: true
    url: https://unbounded.example.com/mcp
    timeout: -5
`

// userMCPConfig makes testMCPYAML this user's ~/.infer/mcp.yaml and returns its path.
func userMCPConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, config.ConfigDirName, config.MCPFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(testMCPYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCreateGivesThePodTheNamedMCPServersAsWritten(t *testing.T) {
	t.Setenv("MAILBOX_API_KEY", "expanded-on-this-machine")
	mcpPath := userMCPConfig(t)
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
			want: map[string]any{"enabled": true, "servers": []any{
				map[string]any{"name": "mailbox", "url": "https://mcp.example.com/mcp", "headers": map[string]any{
					"x-api-key":     "${MAILBOX_API_KEY}",
					"Authorization": "Bearer ${MAILBOX_TOKEN}",
				}},
				map[string]any{"name": "split", "url": "https://split.example.com:8443/rpc"},
			}},
		},
		{
			name:    "a server's tool filters and timeout, and no timeout below one second",
			servers: []string{"filtered", "unbounded"},
			want: map[string]any{"enabled": true, "servers": []any{
				map[string]any{
					"name":           "filtered",
					"url":            "https://filtered.example.com/mcp",
					"includeTools":   []any{"search"},
					"excludeTools":   []any{"delete"},
					"timeoutSeconds": int64(10),
				},
				map[string]any{"name": "unbounded", "url": "https://unbounded.example.com/mcp"},
			}},
		},
		{name: "a name mcp.yaml lacks", servers: []string{"mailbox", "ghost"}, wantErr: "names MCP server ghost, which " + mcpPath + " does not have"},
		{name: "a container server", servers: []string{"container"}, wantErr: "names MCP server container, a container on this machine"},
		{name: "a header holding a value", servers: []string{"leaky"}, wantErr: "MCP server leaky gives header x-api-key a value rather than a ${VAR} reference"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, dynamic := testWorkspace(t, true)
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

func TestCreateNamesTheOperatorVersionMCPServersNeed(t *testing.T) {
	userMCPConfig(t)
	e, dynamic := testWorkspace(t, true)
	e.entry.MCPServers = []string{"mailbox"}
	dynamic.PrependReactor("create", orchestratorResource, func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		invalid := k8sfield.TypeInvalid(k8sfield.NewPath("spec", "mcp", "servers").Index(0), "object", "must be of type string")
		return true, nil, k8serrors.NewInvalid(orchestratorGVR.GroupVersion().WithKind(orchestratorKind).GroupKind(), "infer-1", k8sfield.ErrorList{invalid})
	})
	_, err := e.create(t.Context(), "infer-1", agentdomain.Session{ConversationID: "conv-1"})
	if err == nil || !strings.Contains(err.Error(), "may predate "+mcpServersOperatorVersion+", upgrade it") {
		t.Fatalf("create on an older operator = %v", err)
	}
}
