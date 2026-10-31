package kubernetes

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	k8sdynamic "k8s.io/client-go/dynamic"
	k8sclient "k8s.io/client-go/kubernetes"
	k8srest "k8s.io/client-go/rest"
	k8sclientcmd "k8s.io/client-go/tools/clientcmd"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// The operator API this workspace drives, and the labels it writes.
const (
	orchestratorGroup    = "core.inference-gateway.com"
	orchestratorVersion  = "v1alpha1"
	orchestratorKind     = "Orchestrator"
	orchestratorResource = "orchestrators"

	labelWorkspace    = "inference-gateway.com/workspace"
	labelConversation = "inference-gateway.com/conversation"

	bindingPort      = 52789
	readyTimeout     = 5 * time.Minute
	readyPoll        = 2 * time.Second
	workspaceMount   = "/workspace"
	tlsSecretSuffix  = "-tls"
	secretTokenKey   = "token"
	secretCertKey    = "tls.crt"
	secretKeyKey     = "tls.key"
	secretCAKey      = "ca.crt"
	secretClientCert = "client.crt"
	secretClientKey  = "client.key"

	modeRunning   = "Running"
	modeSuspended = "Suspended"
)

var orchestratorGVR = k8sschema.GroupVersionResource{Group: orchestratorGroup, Version: orchestratorVersion, Resource: orchestratorResource}

// unrecoverableReasons are the Ready reasons the operator reports for an
// Orchestrator that never runs without a change to the cluster or its spec.
var unrecoverableReasons = []string{"SandboxCRDMissing", "InvalidSpec"}

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
	typed    k8sclient.Interface
}

// New connects to the cluster the entry names and returns the workspace.
func New(entry config.WorkspaceEntry, identity string, tls *agentinfra.TLSProvider, pki *agentinfra.PKI) (*Workspace, error) {
	restConfig, err := restConfigFor(entry)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := k8sdynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("building the Kubernetes client for workspace %s: %w", entry.Name, err)
	}
	typedClient, err := k8sclient.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("building the Kubernetes client for workspace %s: %w", entry.Name, err)
	}
	return &Workspace{entry: entry, identity: identity, tls: tls, pki: pki, rest: restConfig, dynamic: dynamicClient, typed: typedClient}, nil
}

func restConfigFor(entry config.WorkspaceEntry) (*k8srest.Config, error) {
	rules := k8sclientcmd.NewDefaultClientConfigLoadingRules()
	if entry.Kubeconfig != "" {
		rules.ExplicitPath = entry.Kubeconfig
	}
	overrides := &k8sclientcmd.ConfigOverrides{CurrentContext: entry.Context}
	restConfig, err := k8sclientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading the kubeconfig for workspace %s: %w", entry.Name, err)
	}
	return restConfig, nil
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
	if e.entry.Address != "" {
		return agentdomain.Endpoint{Addr: e.entry.Address, TLS: tlsConfig, Token: token}, nil
	}
	addr, stop, err := e.portForward(ctx, pod)
	if err != nil {
		return agentdomain.Endpoint{}, err
	}
	return agentdomain.Endpoint{Addr: addr, TLS: tlsConfig, Token: token, Close: stop}, nil
}

// requireOperator fails unless the cluster serves Orchestrators, so a teleport
// aborts before it leaves anything behind in a cluster without the operator.
func (e *Workspace) requireOperator(ctx context.Context) error {
	resources, err := e.typed.Discovery().ServerResourcesForGroupVersionWithContext(ctx, orchestratorGroup+"/"+orchestratorVersion)
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("looking for the Inference Gateway operator in the cluster of workspace %s: %w", e.entry.Name, err)
	}
	if err == nil && slices.ContainsFunc(resources.APIResources, func(r metav1.APIResource) bool { return r.Name == orchestratorResource }) {
		return nil
	}
	return fmt.Errorf("the Inference Gateway operator is not installed in the cluster of workspace %s: install %s or later from https://github.com/inference-gateway/operator/releases", e.entry.Name, minOperatorVersion)
}

// minOperatorVersion is the first operator release that runs an Orchestrator
// in a Sandbox from the image's own entrypoint and links it to a Gateway by ref.
const minOperatorVersion = "v0.29.0"

// ensureSecret holds what the pod daemon authenticates with: a binding token
// and the certificates the daemon CA issued.
func (e *Workspace) ensureSecret(ctx context.Context, name string) (string, error) {
	secrets := e.typed.CoreV1().Secrets(e.entry.Namespace)
	existing, err := secrets.Get(ctx, name+tlsSecretSuffix, metav1.GetOptions{})
	if err == nil {
		return string(existing.Data[secretTokenKey]), nil
	}
	if !k8serrors.IsNotFound(err) {
		return "", fmt.Errorf("reading Secret %s: %w", name+tlsSecretSuffix, err)
	}
	token, err := randomToken()
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
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + tlsSecretSuffix, Namespace: e.entry.Namespace, Labels: map[string]string{labelWorkspace: e.entry.Name}},
		Data:       data,
	}
	if _, err := secrets.Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return "", fmt.Errorf("creating Secret %s: %w", secret.Name, err)
	}
	return token, nil
}

// ownSecret makes the Orchestrator the owner of its Secret, so the garbage
// collector removes the Secret with the Orchestrator however that is deleted.
func (e *Workspace) ownSecret(ctx context.Context, orchestrator *k8sunstructured.Unstructured) error {
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"ownerReferences": []metav1.OwnerReference{ownerOf(orchestrator)}}})
	name := orchestrator.GetName() + tlsSecretSuffix
	if _, err := e.typed.CoreV1().Secrets(e.entry.Namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
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

func randomToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// create submits the Orchestrator for a session. Strict field validation makes
// an older operator refuse a field it does not know, such as the Sandbox
// runtime or the gateway ref, instead of silently dropping it.
func (e *Workspace) create(ctx context.Context, name string, session agentdomain.Session) (*k8sunstructured.Unstructured, error) {
	orchestrator, err := e.orchestrator(name, session)
	if err != nil {
		return nil, err
	}
	created, err := e.orchestrators().Create(ctx, orchestrator, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict})
	if k8serrors.IsBadRequest(err) {
		return nil, fmt.Errorf("creating Orchestrator %s: the operator in the cluster of workspace %s may predate %s, upgrade it: %w", name, e.entry.Name, minOperatorVersion, err)
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
		if !ok || ready["type"] != "Ready" {
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
	status := agentdomain.WorkspaceStatus{Name: e.entry.Name, Kind: agentdomain.WorkspaceKindKubernetes, Capabilities: map[string]string{
		"namespace": e.entry.Namespace, "image": e.entry.Image,
	}}
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
	if version, err := e.typed.Discovery().ServerVersion(); err == nil {
		status.Capabilities["kubernetes"] = version.GitVersion
	}
	return status
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
	if err := e.typed.CoreV1().Secrets(e.entry.Namespace).Delete(ctx, name+tlsSecretSuffix, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("deleting Secret %s: %w", name+tlsSecretSuffix, err)
	}
	return nil
}

// orchestrator is the Orchestrator for a session: the infer image as a daemon
// in a Sandbox, its binding authenticated by the session's Secret, with tools
// and A2A on as they are by default outside the cluster.
func (e *Workspace) orchestrator(name string, session agentdomain.Session) (*k8sunstructured.Unstructured, error) {
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
	secret := name + tlsSecretSuffix
	spec := map[string]any{
		"image":   e.entry.Image,
		"runtime": "Sandbox",
		"sandbox": sandbox,
		"daemon": map[string]any{"binding": map[string]any{
			"port":           int64(bindingPort),
			"tokenSecretRef": map[string]any{"name": secret, "key": secretTokenKey},
			"tls":            map[string]any{"secretName": secret, "clientAuth": true},
		}},
		"tools": map[string]any{"enabled": true},
		"a2a":   map[string]any{"enabled": true},
		"env":   e.env(),
	}
	if gateway := e.orchestratorGateway(); gateway != nil {
		spec["gateway"] = gateway
	}
	return &k8sunstructured.Unstructured{Object: map[string]any{
		"apiVersion": orchestratorGroup + "/" + orchestratorVersion,
		"kind":       orchestratorKind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": e.entry.Namespace,
			"labels":    map[string]any{labelWorkspace: e.entry.Name, labelConversation: session.ConversationID},
		},
		"spec": spec,
	}}, nil
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

func portAddr(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}
