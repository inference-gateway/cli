package kubernetes

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net"
	"regexp"
	"slices"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	k8sdynamic "k8s.io/client-go/dynamic"
	k8srest "k8s.io/client-go/rest"

	operatorv1alpha1 "github.com/inference-gateway/operator/api/v1alpha1"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	computek8s "github.com/inference-gateway/cli/internal/compute/infrastructure/kubernetes"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// The operator API this workspace drives, and the labels it writes.
const (
	orchestratorGroup    = computek8s.OrchestratorGroup
	orchestratorVersion  = computek8s.OrchestratorVersion
	orchestratorKind     = "Orchestrator"
	orchestratorResource = "orchestrators"

	labelWorkspace    = "inference-gateway.com/workspace"
	labelCompute      = computek8s.LabelCompute
	labelConversation = "inference-gateway.com/conversation"

	bindingPort       = 52789
	readyTimeout      = 5 * time.Minute
	readyPoll         = 2 * time.Second
	workspaceMount    = "/workspace"
	tlsSecretSuffix   = "-tls"
	secretTokenKey    = "token"
	secretCertKey     = "tls.crt"
	secretKeyKey      = "tls.key"
	secretCAKey       = "ca.crt"
	secretClientCert  = "client.crt"
	secretClientKey   = "client.key"
	secretUserDataKey = "userdata"

	modeRunning   = "Running"
	modeSuspended = "Suspended"
)

var (
	orchestratorGVR = computek8s.OrchestratorGVR
	secretGVR       = k8sschema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	deploymentGVR   = k8sschema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
)

// unrecoverableReasons are the Ready reasons the operator reports for an
// Orchestrator that never runs without a change to the cluster or its spec.
// SandboxSuspended needs one too, which awaitReady makes by resuming it.
var unrecoverableReasons = []string{operatorv1alpha1.ReasonSandboxCRDMissing, operatorv1alpha1.ReasonInvalidSpec}

// Workspace runs sessions as Inference Gateway operator Orchestrators: one per
// session, running the infer daemon in an agent-sandbox Sandbox, reached
// through a port-forward unless the entry names an address.
type Workspace struct {
	entry    config.WorkspaceEntry
	identity string
	tls      *agentinfra.TLSProvider
	pki      *agentinfra.PKI
	rest     *k8srest.Config
	dynamic  k8sdynamic.Interface
	core     *k8srest.RESTClient
}

// New connects to the cluster the entry names and returns the workspace.
func New(entry config.WorkspaceEntry, identity string, tls *agentinfra.TLSProvider, pki *agentinfra.PKI) (*Workspace, error) {
	clients, err := computek8s.Connect(entry.Kubeconfig, entry.Context, "workspace "+entry.Name)
	if err != nil {
		return nil, err
	}
	return &Workspace{entry: entry, identity: identity, tls: tls, pki: pki, rest: clients.Rest, dynamic: clients.Dynamic, core: clients.Core}, nil
}

func (e *Workspace) Name() string { return e.entry.Name }
func (e *Workspace) Kind() agentdomain.WorkspaceKind {
	return agentdomain.WorkspaceKindKubernetes
}

// orchestratorName is the Orchestrator a session runs in on a workspace, stable
// across teleports. Workspaces sharing a namespace never reach each other's.
func orchestratorName(workspace, conversationID string) string {
	sum := sha256.Sum256([]byte(workspace + "/" + conversationID))
	return "infer-" + hex.EncodeToString(sum[:])[:12]
}

func (e *Workspace) orchestrators() k8sdynamic.ResourceInterface {
	return e.dynamic.Resource(orchestratorGVR).Namespace(e.entry.Namespace)
}

func (e *Workspace) secrets() k8sdynamic.ResourceInterface {
	return e.dynamic.Resource(secretGVR).Namespace(e.entry.Namespace)
}

func secretFrom(object *k8sunstructured.Unstructured) (*corev1.Secret, error) {
	var secret corev1.Secret
	if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &secret); err != nil {
		return nil, fmt.Errorf("reading Secret %s: %w", object.GetName(), err)
	}
	return &secret, nil
}

func unstructuredSecret(secret *corev1.Secret) (*k8sunstructured.Unstructured, error) {
	secret.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}
	object, err := k8sruntime.DefaultUnstructuredConverter.ToUnstructured(secret)
	if err != nil {
		return nil, fmt.Errorf("encoding Secret %s: %w", secret.Name, err)
	}
	return &k8sunstructured.Unstructured{Object: object}, nil
}

// Ensure creates or resumes the session's Orchestrator, waits for its daemon to
// be ready, and returns the daemon's binding through a port-forward.
func (e *Workspace) Ensure(ctx context.Context, session agentdomain.Session) (agentdomain.Endpoint, error) {
	return e.reach(ctx, session, true)
}

// Reach resumes the session's Orchestrator like Ensure but never creates one, so
// an Orchestrator its retention deleted is reported instead of replaced by an
// empty one.
func (e *Workspace) Reach(ctx context.Context, session agentdomain.Session) (agentdomain.Endpoint, error) {
	return e.reach(ctx, session, false)
}

func (e *Workspace) reach(ctx context.Context, session agentdomain.Session, create bool) (agentdomain.Endpoint, error) {
	if err := e.requireOperator(ctx); err != nil {
		return agentdomain.Endpoint{}, err
	}
	name := orchestratorName(e.entry.Name, session.ConversationID)
	orchestrator, err := e.orchestrators().Get(ctx, name, metav1.GetOptions{})
	missing := k8serrors.IsNotFound(err)
	switch {
	case missing && !create:
		return agentdomain.Endpoint{}, fmt.Errorf("orchestrator %s of workspace %s is gone: its retention ran out or it was deleted", name, e.entry.Name)
	case err != nil && !missing:
		return agentdomain.Endpoint{}, fmt.Errorf("reading Orchestrator %s: %w", name, err)
	}
	token, err := e.ensureSecret(ctx, name)
	if err != nil {
		return agentdomain.Endpoint{}, err
	}
	if missing {
		if orchestrator, err = e.create(ctx, name, session); err != nil {
			return agentdomain.Endpoint{}, err
		}
	}
	if err := e.ownSecret(ctx, orchestrator); err != nil {
		return agentdomain.Endpoint{}, err
	}
	if create && e.entry.DeploysGateway() {
		if err := e.ensureGateway(ctx, orchestrator); err != nil {
			return agentdomain.Endpoint{}, err
		}
	}
	pod, err := e.awaitReady(ctx, name)
	if err != nil {
		return agentdomain.Endpoint{}, err
	}
	if e.entry.DeploysGateway() {
		if err := e.awaitGateway(ctx); err != nil {
			return agentdomain.Endpoint{}, err
		}
	}
	tlsConfig, err := e.tls.ClientForKubernetes(name)
	if err != nil {
		return agentdomain.Endpoint{}, err
	}
	entry, err := e.entry.ForOrchestrator(name)
	if err != nil {
		return agentdomain.Endpoint{}, err
	}
	if entry.Address != "" {
		return agentdomain.Endpoint{Addr: entry.Address, TLS: tlsConfig, Token: token, Protocol: agentdomain.WorkspaceProtocol(e.entry.EffectiveProtocol())}, nil
	}
	addr, stop, err := e.portForward(ctx, pod)
	if err != nil {
		return agentdomain.Endpoint{}, err
	}
	return agentdomain.Endpoint{Addr: addr, TLS: tlsConfig, Token: token, Close: stop, Protocol: agentdomain.WorkspaceProtocol(e.entry.EffectiveProtocol())}, nil
}

// requireOperator fails unless the cluster serves Orchestrators, so a teleport
// aborts before it leaves anything behind in a cluster without the operator.
// A cluster without the CRD answers the list with not found.
func (e *Workspace) requireOperator(ctx context.Context) error {
	return computek8s.RequireOperator(ctx, e.dynamic, e.entry.Namespace, "the cluster of workspace "+e.entry.Name)
}

// bindingServiceOperatorVersion is the first operator release that copies
// binding_service metadata onto the binding Service.
const bindingServiceOperatorVersion = "v0.33.0"

// mcpServersOperatorVersion is the first operator release that takes all a
// workspace's MCP servers carry: headers, tool filters and a timeout. One with
// headers alone also runs on bindingServiceOperatorVersion.
const mcpServersOperatorVersion = "v0.34.0"

// userDataOperatorVersion is the first operator release that boots a pod with
// cloud-init user-data.
const userDataOperatorVersion = "v0.35.0"

// volumesOperatorVersion is the first operator release that mounts existing
// claims into a session's pod.
const volumesOperatorVersion = "v0.36.0"

func (e *Workspace) requiredOperatorVersion() string {
	switch {
	case len(e.entry.Volumes) > 0:
		return volumesOperatorVersion
	case e.entry.UserData != "":
		return userDataOperatorVersion
	case len(e.entry.MCPServers) > 0:
		return mcpServersOperatorVersion
	case !e.entry.BindingService.IsZero():
		return bindingServiceOperatorVersion
	default:
		return computek8s.MinOperatorVersion
	}
}

// ensureSecret holds what the pod daemon authenticates with, a binding token
// and the certificates the daemon CA issued, and the user-data the pod boots
// with, fixed for the session like an instance's.
func (e *Workspace) ensureSecret(ctx context.Context, name string) (string, error) {
	existing, err := e.secrets().Get(ctx, name+tlsSecretSuffix, metav1.GetOptions{})
	if err == nil {
		secret, err := secretFrom(existing)
		if err != nil {
			return "", err
		}
		return string(secret.Data[secretTokenKey]), nil
	}
	if !k8serrors.IsNotFound(err) {
		return "", fmt.Errorf("reading Secret %s: %w", name+tlsSecretSuffix, err)
	}
	token, err := utils.RandomToken()
	if err != nil {
		return "", err
	}
	if e.pki == nil {
		return "", errors.New("a kubernetes workspace needs the daemon CA")
	}
	server, err := e.pki.IssueServer(name, name+"."+e.entry.Namespace, name+"."+e.entry.Namespace+".svc")
	if err != nil {
		return "", err
	}
	client, err := e.pki.IssueClient(name)
	if err != nil {
		return "", err
	}
	data := map[string][]byte{
		secretTokenKey:   []byte(token),
		secretCertKey:    server.CertPEM,
		secretKeyKey:     server.KeyPEM,
		secretCAKey:      server.CAPEM,
		secretClientCert: client.CertPEM,
		secretClientKey:  client.KeyPEM,
	}
	if e.entry.UserData != "" {
		data[secretUserDataKey] = []byte(e.entry.UserData)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + tlsSecretSuffix, Namespace: e.entry.Namespace, Labels: map[string]string{labelWorkspace: e.entry.Name}},
		Data:       data,
	}
	object, err := unstructuredSecret(secret)
	if err != nil {
		return "", err
	}
	if _, err := e.secrets().Create(ctx, object, metav1.CreateOptions{}); err != nil {
		return "", fmt.Errorf("creating Secret %s: %w", secret.Name, err)
	}
	return token, nil
}

// ownSecret makes the Orchestrator the owner of its Secret, so the garbage
// collector removes the Secret with the Orchestrator however that is deleted.
func (e *Workspace) ownSecret(ctx context.Context, orchestrator *k8sunstructured.Unstructured) error {
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"ownerReferences": []metav1.OwnerReference{ownerOf(orchestrator)}}})
	name := orchestrator.GetName() + tlsSecretSuffix
	if _, err := e.secrets().Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("making Orchestrator %s the owner of Secret %s: %w", orchestrator.GetName(), name, err)
	}
	return nil
}

func ownerOf(orchestrator *k8sunstructured.Unstructured) metav1.OwnerReference {
	return metav1.OwnerReference{APIVersion: orchestratorGroup + "/" + orchestratorVersion, Kind: orchestratorKind, Name: orchestrator.GetName(), UID: orchestrator.GetUID()}
}

// addOwner adds the Orchestrator to an object's owners unless it is one, so an
// object the sessions share goes with the last of them.
func addOwner(owners []metav1.OwnerReference, orchestrator *k8sunstructured.Unstructured) []metav1.OwnerReference {
	if slices.ContainsFunc(owners, func(owner metav1.OwnerReference) bool { return owner.UID == orchestrator.GetUID() }) {
		return owners
	}
	return append(slices.Clone(owners), ownerOf(orchestrator))
}

// create submits the Orchestrator for a session. Strict field validation makes
// an older operator refuse a field it does not know, such as the Sandbox
// runtime or the gateway ref, or one it types differently, such as an MCP
// server, instead of silently dropping it.
func (e *Workspace) create(ctx context.Context, name string, session agentdomain.Session) (*k8sunstructured.Unstructured, error) {
	secretEnv, err := e.secretEnv(ctx)
	if err != nil {
		return nil, err
	}
	mcpServers, err := e.mcpServers()
	if err != nil {
		return nil, err
	}
	orchestrator, err := e.orchestrator(name, session, secretEnv, mcpServers)
	if err != nil {
		return nil, err
	}
	created, err := e.orchestrators().Create(ctx, orchestrator, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict})
	if k8serrors.IsBadRequest(err) || k8serrors.IsInvalid(err) {
		return nil, fmt.Errorf("creating Orchestrator %s: the operator in the cluster of workspace %s may predate %s, upgrade it: %w", name, e.entry.Name, e.requiredOperatorVersion(), err)
	}
	if err != nil {
		return nil, fmt.Errorf("creating Orchestrator %s: %w", name, err)
	}
	logger.Info("created an Orchestrator for the session", "workspace", e.entry.Name, "orchestrator", name, "conversation_id", session.ConversationID)
	return created, nil
}

// setMode runs or suspends the Orchestrator's Sandbox. The operator keeps the
// volumes across a suspension and owns the expiry.
func (e *Workspace) setMode(ctx context.Context, name, mode string) error {
	patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"sandbox": map[string]any{"operatingMode": mode}}})
	if _, err := e.orchestrators().Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("setting Orchestrator %s to %s: %w", name, mode, err)
	}
	return nil
}

// awaitReady polls until the Orchestrator's daemon is ready and returns the pod
// it runs. It resumes an Orchestrator found suspended, also one the operator
// parks during the wait, and gives up early on a reason that never recovers.
func (e *Workspace) awaitReady(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	ticker := time.NewTicker(readyPoll)
	defer ticker.Stop()
	for {
		orchestrator, err := e.orchestrators().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("reading Orchestrator %s: %w", name, err)
		}
		ready := readyCondition(orchestrator)
		switch {
		case isSuspended(orchestrator):
			if err := e.setMode(ctx, name, modeRunning); err != nil {
				return "", err
			}
		case ready.status == string(metav1.ConditionTrue):
			pod, _, _ := k8sunstructured.NestedString(orchestrator.Object, "status", "podName")
			return cmp.Or(pod, name), nil
		case slices.Contains(unrecoverableReasons, ready.reason):
			return "", fmt.Errorf("orchestrator %s cannot run: %s", name, ready.message)
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("orchestrator %s did not become ready in time: %s", name, cmp.Or(ready.message, "no condition reported"))
		case <-ticker.C:
		}
	}
}

func isSuspended(orchestrator *k8sunstructured.Unstructured) bool {
	mode, _, _ := k8sunstructured.NestedString(orchestrator.Object, "spec", "sandbox", "operatingMode")
	return mode == modeSuspended
}

type condition struct{ status, reason, message string }

// readyCondition is the Ready condition the operator reported for the
// Orchestrator's current generation. One about an older spec reads as unknown,
// since it says nothing about the change that was just made.
func readyCondition(orchestrator *k8sunstructured.Unstructured) condition {
	conditions, _, _ := k8sunstructured.NestedSlice(orchestrator.Object, "status", "conditions")
	for _, raw := range conditions {
		ready, ok := raw.(map[string]any)
		if !ok || ready["type"] != operatorv1alpha1.OrchestratorConditionReady {
			continue
		}
		if observed, found, _ := k8sunstructured.NestedInt64(ready, "observedGeneration"); found && observed != orchestrator.GetGeneration() {
			return condition{}
		}
		status, _ := ready["status"].(string)
		reason, _ := ready["reason"].(string)
		message, _ := ready["message"].(string)
		return condition{status: status, reason: reason, message: message}
	}
	return condition{}
}

// Probe lists this workspace's Orchestrators and the cluster version. A missing
// operator or an unreachable cluster is the error.
func (e *Workspace) Probe(ctx context.Context) agentdomain.WorkspaceStatus {
	status := agentdomain.WorkspaceStatus{
		Name: e.entry.Name, Compute: e.entry.Compute, Kind: agentdomain.WorkspaceKindKubernetes, Protocol: agentdomain.WorkspaceProtocol(e.entry.EffectiveProtocol()),
		Capabilities: map[string]string{
			"namespace": e.entry.Namespace, "image": e.entry.Image,
		},
	}
	if err := e.requireOperator(ctx); err != nil {
		status.Error = err.Error()
		return status
	}
	list, err := e.orchestrators().List(ctx, metav1.ListOptions{LabelSelector: labelWorkspace + "=" + e.entry.Name})
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Reachable = true
	status.Capabilities["orchestrators"] = strconv.Itoa(len(list.Items))
	if version, err := e.serverVersion(ctx); err == nil {
		status.Capabilities["kubernetes"] = version
	}
	return status
}

func (e *Workspace) serverVersion(ctx context.Context) (string, error) {
	return computek8s.ServerVersion(ctx, e.core)
}

// Suspend parks the session's Orchestrator, keeping its volumes.
func (e *Workspace) Suspend(ctx context.Context, session agentdomain.Session) error {
	return e.setMode(ctx, orchestratorName(e.entry.Name, session.ConversationID), modeSuspended)
}

// Delete removes the session's Orchestrator and its Secret. The operator's
// Sandbox and its volumes go with the Orchestrator.
func (e *Workspace) Delete(ctx context.Context, session agentdomain.Session) error {
	name := orchestratorName(e.entry.Name, session.ConversationID)
	if err := e.orchestrators().Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("deleting Orchestrator %s: %w", name, err)
	}
	if err := e.secrets().Delete(ctx, name+tlsSecretSuffix, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("deleting Secret %s: %w", name+tlsSecretSuffix, err)
	}
	return nil
}

// orchestrator is the Orchestrator for a session: the infer image as a daemon
// in a Sandbox, its binding authenticated by and its user-data read from the
// session's Secret, with tools and A2A on as they are by default outside the
// cluster.
func (e *Workspace) orchestrator(name string, session agentdomain.Session, secretEnv, mcpServers []any) (*k8sunstructured.Unstructured, error) {
	sandbox := map[string]any{"operatingMode": modeRunning}
	if e.entry.Storage != "" {
		if _, err := k8sresource.ParseQuantity(e.entry.Storage); err != nil {
			return nil, fmt.Errorf("invalid storage %q for workspace %s: %w", e.entry.Storage, e.entry.Name, err)
		}
		sandbox["storage"] = e.entry.Storage
	}
	if e.entry.Retention > 0 {
		sandbox["retention"] = e.entry.Retention.String()
	}
	if len(e.entry.Volumes) > 0 {
		sandbox["volumes"] = sandboxVolumes(e.entry.Volumes)
	}
	secret := name + tlsSecretSuffix
	binding := map[string]any{
		"port":           int64(bindingPort),
		"tokenSecretRef": map[string]any{"name": secret, "key": secretTokenKey},
		"tls":            map[string]any{"secretName": secret, "clientAuth": true},
	}
	if !e.entry.BindingService.IsZero() {
		entry, err := e.entry.ForOrchestrator(name)
		if err != nil {
			return nil, err
		}
		binding["service"] = bindingService(entry.BindingService)
	}
	spec := map[string]any{
		"image":   e.entry.Image,
		"runtime": "Sandbox",
		"sandbox": sandbox,
		"daemon":  map[string]any{"binding": binding},
		"tools":   map[string]any{"enabled": true},
		"a2a":     map[string]any{"enabled": true},
		"env":     slices.Concat(e.env(), secretEnv),
	}
	if gateway := e.orchestratorGateway(); gateway != nil {
		spec["gateway"] = gateway
	}
	if len(mcpServers) > 0 {
		spec["mcp"] = map[string]any{"enabled": true, "servers": mcpServers}
	}
	if e.entry.UserData != "" {
		spec["userData"] = map[string]any{"secretRef": map[string]any{"name": secret, "key": secretUserDataKey}}
	}
	return &k8sunstructured.Unstructured{Object: map[string]any{
		"apiVersion": orchestratorGroup + "/" + orchestratorVersion,
		"kind":       orchestratorKind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": e.entry.Namespace,
			"labels":    e.orchestratorLabels(session),
		},
		"spec": spec,
	}}, nil
}

// sandboxVolumes are a workspace's volumes as the Orchestrator's sandbox
// takes them: existing claims of the namespace, which it never owns.
func sandboxVolumes(volumes []config.WorkspaceVolume) []any {
	out := make([]any, 0, len(volumes))
	for _, volume := range volumes {
		entry := map[string]any{"name": volume.Name, "claimName": volume.Claim, "mountPath": volume.MountPath}
		if volume.ReadOnly {
			entry["readOnly"] = true
		}
		out = append(out, entry)
	}
	return out
}

// orchestratorLabels mark a session's Orchestrator with its workspace and
// conversation, and with its cluster when it runs on one, so a cluster's
// status counts the sessions of every workspace on it.
func (e *Workspace) orchestratorLabels(session agentdomain.Session) map[string]any {
	labels := map[string]any{labelWorkspace: e.entry.Name, labelConversation: session.ConversationID}
	if e.entry.Compute != "" {
		labels[labelCompute] = e.entry.Compute
	}
	return labels
}

// env is what the pod daemon needs beyond what the operator sets: which
// workspace it is, where projects restore, when it idles out with and without
// its sessions, then the entry's.
func (e *Workspace) env() []any {
	env := []any{
		envVar("INFER_DAEMON_WORKSPACE", e.entry.Name),
		envVar("INFER_DAEMON_PROJECTS_ROOT", workspaceMount),
		envVar("INFER_DAEMON_IDLE_EXIT", e.entry.EffectiveIdleTTL().String()),
		envVar("INFER_DAEMON_AWAY_EXIT", e.entry.EffectiveAwayTTL().String()),
	}
	for _, key := range slices.Sorted(maps.Keys(e.entry.Env)) {
		env = append(env, envVar(key, e.entry.Env[key]))
	}
	return env
}

func envVar(name, value string) map[string]any {
	return map[string]any{"name": name, "value": value}
}

// secretEnv references every key of the entry's Secrets, read when the
// Orchestrator is created. The pod resolves the values itself, so they never
// pass through this machine, and a rotated value reaches the next pod start.
func (e *Workspace) secretEnv(ctx context.Context) ([]any, error) {
	var env []any
	for _, name := range e.entry.Secrets {
		object, err := e.secrets().Get(ctx, name, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("workspace %s names Secret %s, which namespace %s does not have", e.entry.Name, name, e.entry.Namespace)
		}
		if err != nil {
			return nil, fmt.Errorf("reading Secret %s of workspace %s: %w", name, e.entry.Name, err)
		}
		secret, err := secretFrom(object)
		if err != nil {
			return nil, err
		}
		for _, key := range slices.Sorted(maps.Keys(secret.Data)) {
			env = append(env, secretEnvVar(key, name))
		}
	}
	return env, nil
}

// headerReference is a header value that names a variable rather than holding
// a value: a ${VAR} reference with an optional prefix, such as "Bearer ".
var headerReference = regexp.MustCompile(`^[^$]*\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)

// mcpServers are the servers of ~/.infer/mcp.yaml the entry names, read as
// written when the Orchestrator is created, so a header stays a ${VAR} reference
// the pod expands from its own Secrets and no value lands in a Kubernetes object.
// Each keeps its tool filters and timeout. A name mcp.yaml lacks, a container
// server and a header holding a value fail.
func (e *Workspace) mcpServers() ([]any, error) {
	if len(e.entry.MCPServers) == 0 {
		return nil, nil
	}
	path := config.UserMCPPath()
	cfg, err := config.LoadMCPForEdit(path)
	if err != nil {
		return nil, fmt.Errorf("reading the MCP servers of workspace %s: %w", e.entry.Name, err)
	}
	servers := make([]any, 0, len(e.entry.MCPServers))
	for _, name := range e.entry.MCPServers {
		server, err := cfg.ReadEntry(name)
		if err != nil {
			return nil, fmt.Errorf("workspace %s names MCP server %s, which %s does not have", e.entry.Name, name, path)
		}
		if server.Run {
			return nil, fmt.Errorf("workspace %s names MCP server %s, a container on this machine, and only a hosted server reaches its pods", e.entry.Name, name)
		}
		for key, value := range server.Headers {
			if !headerReference.MatchString(value) {
				return nil, fmt.Errorf("MCP server %s gives header %s a value rather than a ${VAR} reference, which workspace %s would write into its Orchestrator", name, key, e.entry.Name)
			}
		}
		entry, err := k8sruntime.DefaultUnstructuredConverter.ToUnstructured(&operatorv1alpha1.OrchestratorMCPServer{
			Name:           name,
			URL:            server.GetURL(),
			Headers:        server.Headers,
			IncludeTools:   server.IncludeTools,
			ExcludeTools:   server.ExcludeTools,
			TimeoutSeconds: int32(min(max(server.Timeout, 0), math.MaxInt32)),
		})
		if err != nil {
			return nil, fmt.Errorf("writing MCP server %s for workspace %s: %w", name, e.entry.Name, err)
		}
		servers = append(servers, entry)
	}
	return servers, nil
}

func secretEnvVar(key, secret string) map[string]any {
	return map[string]any{"name": key, "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": secret, "key": key}}}
}

// bindingService is the Service metadata as an unstructured value, which
// takes map[string]any and no empty sub-map.
func bindingService(service config.BindingServiceConfig) map[string]any {
	out := map[string]any{}
	if len(service.Annotations) > 0 {
		out["annotations"] = stringMap(service.Annotations)
	}
	if len(service.Labels) > 0 {
		out["labels"] = stringMap(service.Labels)
	}
	return out
}

func stringMap(values map[string]string) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func portAddr(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}
