package kubernetes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	k8sclient "k8s.io/client-go/kubernetes"
	k8srest "k8s.io/client-go/rest"
	k8sclientcmd "k8s.io/client-go/tools/clientcmd"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// The agent-sandbox API this environment drives, and the labels and annotation
// it reads and writes.
const (
	sandboxGroup   = "agents.x-k8s.io"
	sandboxVersion = "v1beta1"
	sandboxKind    = "Sandbox"

	podNameAnnotation = "agents.x-k8s.io/pod-name"
	labelEnvironment  = "inference-gateway.com/environment"
	labelConversation = "inference-gateway.com/conversation"

	bindingPort      = 52789
	readyTimeout     = 5 * time.Minute
	readyPoll        = 2 * time.Second
	containerName    = "infer"
	workspaceMount   = "/workspace"
	stateMount       = "/home/infer/.infer/projects"
	tlsMount         = "/etc/infer/tls"
	spiffeMount      = "/spiffe-workload-api"
	serviceAccount   = "infer-environment"
	defaultStorage   = "5Gi"
	tlsSecretSuffix  = "-tls"
	secretTokenKey   = "token"
	secretCertKey    = "tls.crt"
	secretKeyKey     = "tls.key"
	secretCAKey      = "ca.crt"
	secretClientCert = "client.crt"
	secretClientKey  = "client.key"
)

var sandboxGVR = k8sschema.GroupVersionResource{Group: sandboxGroup, Version: sandboxVersion, Resource: "sandboxes"}

// Environment runs sessions as agent-sandbox Sandboxes: one per session, running
// the infer image as a daemon, reached through a port-forward unless the
// entry names an address.
type Environment struct {
	entry    config.EnvironmentEntry
	identity string
	tls      *agentinfra.TLSProvider
	pki      *agentinfra.PKI
	rest     *k8srest.Config
	dynamic  k8sdynamic.Interface
	typed    k8sclient.Interface
}

// New connects to the cluster the entry names and returns the environment.
func New(entry config.EnvironmentEntry, identity string, tls *agentinfra.TLSProvider, pki *agentinfra.PKI) (*Environment, error) {
	restConfig, err := restConfigFor(entry)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := k8sdynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("building the Kubernetes client for environment %s: %w", entry.Name, err)
	}
	typedClient, err := k8sclient.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("building the Kubernetes client for environment %s: %w", entry.Name, err)
	}
	return &Environment{entry: entry, identity: identity, tls: tls, pki: pki, rest: restConfig, dynamic: dynamicClient, typed: typedClient}, nil
}

func restConfigFor(entry config.EnvironmentEntry) (*k8srest.Config, error) {
	rules := k8sclientcmd.NewDefaultClientConfigLoadingRules()
	if entry.Kubeconfig != "" {
		rules.ExplicitPath = entry.Kubeconfig
	}
	overrides := &k8sclientcmd.ConfigOverrides{CurrentContext: entry.Context}
	restConfig, err := k8sclientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading the kubeconfig for environment %s: %w", entry.Name, err)
	}
	return restConfig, nil
}

func (e *Environment) Name() string { return e.entry.Name }
func (e *Environment) Kind() agentdomain.EnvironmentKind {
	return agentdomain.EnvironmentKindKubernetes
}

// SandboxName is the Sandbox a session runs in, stable across transfers.
func SandboxName(conversationID string) string {
	sum := sha256.Sum256([]byte(conversationID))
	return "infer-" + hex.EncodeToString(sum[:])[:12]
}

// Ensure creates or resumes the session's Sandbox, waits for it to be Ready,
// and returns the pod daemon's binding through a port-forward.
func (e *Environment) Ensure(ctx context.Context, session agentdomain.Session) (agentdomain.Endpoint, error) {
	return e.reach(ctx, session, true)
}

// Reach resumes the session's Sandbox like Ensure but never creates one, so a
// Sandbox its retention deleted is reported instead of replaced by an empty one.
func (e *Environment) Reach(ctx context.Context, session agentdomain.Session) (agentdomain.Endpoint, error) {
	return e.reach(ctx, session, false)
}

func (e *Environment) reach(ctx context.Context, session agentdomain.Session, create bool) (agentdomain.Endpoint, error) {
	name := SandboxName(session.ConversationID)
	sandbox, err := e.dynamic.Resource(sandboxGVR).Namespace(e.entry.Namespace).Get(ctx, name, metav1.GetOptions{})
	missing := k8serrors.IsNotFound(err)
	switch {
	case missing && !create:
		return agentdomain.Endpoint{}, fmt.Errorf("sandbox %s of environment %s is gone: its retention ran out or it was deleted", name, e.entry.Name)
	case err != nil && !missing:
		return agentdomain.Endpoint{}, fmt.Errorf("reading Sandbox %s: %w", name, err)
	}
	token, err := e.ensureSecret(ctx, name)
	if err != nil {
		return agentdomain.Endpoint{}, err
	}
	if missing {
		sandbox, err = e.create(ctx, name, session)
	} else {
		err = e.resume(ctx, sandbox)
	}
	if err != nil {
		return agentdomain.Endpoint{}, err
	}
	if err := e.ownSecret(ctx, sandbox); err != nil {
		return agentdomain.Endpoint{}, err
	}
	pod, err := e.awaitReady(ctx, name)
	if err != nil {
		return agentdomain.Endpoint{}, err
	}
	tlsConfig, err := e.tls.ClientForKubernetes(ctx, e.entry, name)
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

// ensureSecret holds what the pod daemon authenticates with: a binding token
// always, plus the certificates the daemon CA issued when auth is ca.
func (e *Environment) ensureSecret(ctx context.Context, name string) (string, error) {
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
	data := map[string][]byte{secretTokenKey: []byte(token)}
	if e.entry.EffectiveAuth() == config.EnvironmentAuthCA {
		if e.pki == nil {
			return "", errors.New("environment auth ca needs the daemon CA")
		}
		server, err := e.pki.IssueServer(name, name+"."+e.entry.Namespace, name+"."+e.entry.Namespace+".svc")
		if err != nil {
			return "", err
		}
		client, err := e.pki.IssueClient(name)
		if err != nil {
			return "", err
		}
		data[secretCertKey], data[secretKeyKey], data[secretCAKey] = server.CertPEM, server.KeyPEM, server.CAPEM
		data[secretClientCert], data[secretClientKey] = client.CertPEM, client.KeyPEM
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + tlsSecretSuffix, Namespace: e.entry.Namespace, Labels: map[string]string{labelEnvironment: e.entry.Name}},
		Data:       data,
	}
	if _, err := secrets.Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return "", fmt.Errorf("creating Secret %s: %w", secret.Name, err)
	}
	return token, nil
}

// ownSecret makes the Sandbox the owner of its Secret, so the garbage
// collector removes the Secret with the Sandbox however that is deleted.
func (e *Environment) ownSecret(ctx context.Context, sandbox *k8sunstructured.Unstructured) error {
	owner := metav1.OwnerReference{APIVersion: sandboxGroup + "/" + sandboxVersion, Kind: sandboxKind, Name: sandbox.GetName(), UID: sandbox.GetUID()}
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"ownerReferences": []metav1.OwnerReference{owner}}})
	name := sandbox.GetName() + tlsSecretSuffix
	if _, err := e.typed.CoreV1().Secrets(e.entry.Namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("making Sandbox %s the owner of Secret %s: %w", sandbox.GetName(), name, err)
	}
	return nil
}

func randomToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// create submits the Sandbox for a session.
func (e *Environment) create(ctx context.Context, name string, session agentdomain.Session) (*k8sunstructured.Unstructured, error) {
	sandbox, err := e.sandbox(name, session)
	if err != nil {
		return nil, err
	}
	created, err := e.dynamic.Resource(sandboxGVR).Namespace(e.entry.Namespace).Create(ctx, sandbox, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("creating Sandbox %s: %w", name, err)
	}
	logger.Info("created a Sandbox for the session", "environment", e.entry.Name, "sandbox", name, "conversation_id", session.ConversationID)
	return created, nil
}

// resume brings a parked or finished Sandbox back to Running: a suspended one
// is set Running, a finished one cycles through Suspended first so a fresh pod
// reattaches the volumes.
func (e *Environment) resume(ctx context.Context, sandbox *k8sunstructured.Unstructured) error {
	mode, _, _ := k8sunstructured.NestedString(sandbox.Object, "spec", "operatingMode")
	ready := conditionTrue(sandbox, "Ready")
	finished := conditionTrue(sandbox, "Finished")
	switch {
	case mode == "Suspended":
		return e.setMode(ctx, sandbox.GetName(), "Running")
	case !ready && finished:
		if err := e.setMode(ctx, sandbox.GetName(), "Suspended"); err != nil {
			return err
		}
		if err := e.awaitCondition(ctx, sandbox.GetName(), "Suspended"); err != nil {
			return err
		}
		return e.setMode(ctx, sandbox.GetName(), "Running")
	}
	return nil
}

// setMode moves the Sandbox to mode. Running also clears the expiry a
// suspension may have set, so a resumed session is never deleted under its pod.
func (e *Environment) setMode(ctx context.Context, name, mode string) error {
	spec := map[string]any{"operatingMode": mode}
	if mode == "Running" {
		spec["shutdownTime"], spec["shutdownPolicy"] = nil, nil
	}
	patch, _ := json.Marshal(map[string]any{"spec": spec})
	if _, err := e.dynamic.Resource(sandboxGVR).Namespace(e.entry.Namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("setting Sandbox %s to %s: %w", name, mode, err)
	}
	return nil
}

// awaitReady polls until the Sandbox reports Ready and returns its pod name.
func (e *Environment) awaitReady(ctx context.Context, name string) (string, error) {
	if err := e.awaitCondition(ctx, name, "Ready"); err != nil {
		return "", err
	}
	sandbox, err := e.dynamic.Resource(sandboxGVR).Namespace(e.entry.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("reading Sandbox %s: %w", name, err)
	}
	pod := sandbox.GetAnnotations()[podNameAnnotation]
	if pod == "" {
		pod = name
	}
	return pod, nil
}

func (e *Environment) awaitCondition(ctx context.Context, name, condition string) error {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	ticker := time.NewTicker(readyPoll)
	defer ticker.Stop()
	for {
		sandbox, err := e.dynamic.Resource(sandboxGVR).Namespace(e.entry.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("reading Sandbox %s: %w", name, err)
		}
		if conditionTrue(sandbox, condition) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("sandbox %s did not become %s in time: %s", name, condition, conditionMessage(sandbox, condition))
		case <-ticker.C:
		}
	}
}

func conditionTrue(sandbox *k8sunstructured.Unstructured, name string) bool {
	return conditionStatus(sandbox, name) == string(metav1.ConditionTrue)
}

func conditionStatus(sandbox *k8sunstructured.Unstructured, name string) string {
	conditions, _, _ := k8sunstructured.NestedSlice(sandbox.Object, "status", "conditions")
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if ok && condition["type"] == name {
			status, _ := condition["status"].(string)
			return status
		}
	}
	return ""
}

func conditionMessage(sandbox *k8sunstructured.Unstructured, name string) string {
	conditions, _, _ := k8sunstructured.NestedSlice(sandbox.Object, "status", "conditions")
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if ok && condition["type"] == name {
			message, _ := condition["message"].(string)
			return message
		}
	}
	return "no condition reported"
}

// Probe lists this environment's Sandboxes and the cluster version. A missing
// CRD or an unreachable cluster is the error.
func (e *Environment) Probe(ctx context.Context) agentdomain.EnvironmentStatus {
	status := agentdomain.EnvironmentStatus{Name: e.entry.Name, Kind: agentdomain.EnvironmentKindKubernetes, Capabilities: map[string]string{
		"namespace": e.entry.Namespace, "image": e.entry.Image, "auth": string(e.entry.EffectiveAuth()),
	}}
	list, err := e.dynamic.Resource(sandboxGVR).Namespace(e.entry.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labelEnvironment + "=" + e.entry.Name})
	if err != nil {
		status.Error = describeClusterError(err)
		return status
	}
	status.Reachable = true
	status.Capabilities["sandboxes"] = strconv.Itoa(len(list.Items))
	if version, err := e.typed.Discovery().ServerVersion(); err == nil {
		status.Capabilities["kubernetes"] = version.GitVersion
	}
	return status
}

func describeClusterError(err error) string {
	if k8serrors.IsNotFound(err) {
		return "the agent-sandbox CRD is not installed: " + err.Error()
	}
	return err.Error()
}

// Suspend parks the session's Sandbox, keeping its volumes.
func (e *Environment) Suspend(ctx context.Context, session agentdomain.Session) error {
	return e.setMode(ctx, SandboxName(session.ConversationID), "Suspended")
}

// Delete removes the session's Sandbox and its Secret. The volumes go with
// the Sandbox.
func (e *Environment) Delete(ctx context.Context, session agentdomain.Session) error {
	name := SandboxName(session.ConversationID)
	if err := e.dynamic.Resource(sandboxGVR).Namespace(e.entry.Namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("deleting Sandbox %s: %w", name, err)
	}
	if err := e.typed.CoreV1().Secrets(e.entry.Namespace).Delete(ctx, name+tlsSecretSuffix, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("deleting Secret %s: %w", name+tlsSecretSuffix, err)
	}
	return nil
}

// sandbox is the Sandbox object for a session: the infer image as a daemon
// with its binding on the pod network, a workspace and a state volume, and
// the authentication the entry chose.
func (e *Environment) sandbox(name string, session agentdomain.Session) (*k8sunstructured.Unstructured, error) {
	podSpec := e.podSpec(name)
	spec, err := k8sruntime.DefaultUnstructuredConverter.ToUnstructured(&podSpec)
	if err != nil {
		return nil, err
	}
	storage := e.entry.Storage
	if storage == "" {
		storage = defaultStorage
	}
	if _, err := k8sresource.ParseQuantity(storage); err != nil {
		return nil, fmt.Errorf("invalid storage %q for environment %s: %w", storage, e.entry.Name, err)
	}
	claim := func(volume string) map[string]any {
		return map[string]any{
			"metadata": map[string]any{"name": volume},
			"spec": map[string]any{
				"accessModes": []any{"ReadWriteOnce"},
				"resources":   map[string]any{"requests": map[string]any{"storage": storage}},
			},
		}
	}
	return &k8sunstructured.Unstructured{Object: map[string]any{
		"apiVersion": sandboxGroup + "/" + sandboxVersion,
		"kind":       sandboxKind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": e.entry.Namespace,
			"labels":    map[string]any{labelEnvironment: e.entry.Name, labelConversation: session.ConversationID},
		},
		"spec": map[string]any{
			"podTemplate":          map[string]any{"metadata": map[string]any{"labels": map[string]any{labelEnvironment: e.entry.Name}}, "spec": spec},
			"volumeClaimTemplates": []any{claim("workspace"), claim("state")},
			"failedPodPolicy":      "Recreate",
		},
	}}, nil
}

func (e *Environment) podSpec(name string) corev1.PodSpec {
	fsGroup := int64(1000)
	env := []corev1.EnvVar{
		{Name: "INFER_DAEMON_BINDING_ENABLED", Value: "true"},
		{Name: "INFER_DAEMON_BINDING_HOST", Value: "0.0.0.0"},
		{Name: "INFER_DAEMON_BINDING_PORT", Value: strconv.Itoa(bindingPort)},
		{Name: "INFER_DAEMON_ENVIRONMENT", Value: e.entry.Name},
		{Name: "INFER_DAEMON_WORKSPACE_ROOT", Value: workspaceMount},
		{Name: "INFER_DAEMON_IDLE_EXIT", Value: e.entry.EffectiveIdleTTL().String()},
		{Name: EnvSandboxName, Value: name},
		{Name: EnvSandboxRetention, Value: e.entry.Retention.String()},
		{Name: EnvSandboxNamespace, ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}},
		{Name: "INFER_DAEMON_BINDING_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: name + tlsSecretSuffix}, Key: secretTokenKey,
		}}},
	}
	for key, value := range e.entry.Env {
		env = append(env, corev1.EnvVar{Name: key, Value: value})
	}
	volumes := []corev1.Volume{}
	mounts := []corev1.VolumeMount{{Name: "workspace", MountPath: workspaceMount}, {Name: "state", MountPath: stateMount}}
	switch e.entry.EffectiveAuth() {
	case config.EnvironmentAuthSPIFFE:
		env = append(env,
			corev1.EnvVar{Name: "INFER_DAEMON_BINDING_TLS_MODE", Value: string(config.TLSModeSPIFFE)},
			corev1.EnvVar{Name: "INFER_DAEMON_BINDING_TLS_SPIFFE_TRUST_DOMAIN", Value: e.entry.SPIFFE.TrustDomain},
			corev1.EnvVar{Name: "INFER_DAEMON_BINDING_TLS_SPIFFE_SOCKET", Value: "unix://" + spiffeMount + "/spire-agent.sock"},
		)
		readOnly := true
		volumes = append(volumes, corev1.Volume{Name: "spiffe-workload-api", VolumeSource: corev1.VolumeSource{
			CSI: &corev1.CSIVolumeSource{Driver: "csi.spiffe.io", ReadOnly: &readOnly},
		}})
		mounts = append(mounts, corev1.VolumeMount{Name: "spiffe-workload-api", MountPath: spiffeMount, ReadOnly: true})
	default:
		env = append(env,
			corev1.EnvVar{Name: "INFER_DAEMON_BINDING_TLS_MODE", Value: string(config.TLSModeFiles)},
			corev1.EnvVar{Name: "INFER_DAEMON_BINDING_TLS_CERT_FILE", Value: tlsMount + "/" + secretCertKey},
			corev1.EnvVar{Name: "INFER_DAEMON_BINDING_TLS_KEY_FILE", Value: tlsMount + "/" + secretKeyKey},
			corev1.EnvVar{Name: "INFER_DAEMON_BINDING_TLS_CA_FILE", Value: tlsMount + "/" + secretCAKey},
			corev1.EnvVar{Name: "INFER_DAEMON_BINDING_TLS_CLIENT_AUTH", Value: "true"},
		)
		volumes = append(volumes, corev1.Volume{Name: "tls", VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{SecretName: name + tlsSecretSuffix},
		}})
		mounts = append(mounts, corev1.VolumeMount{Name: "tls", MountPath: tlsMount, ReadOnly: true})
	}
	return corev1.PodSpec{
		ServiceAccountName: serviceAccount,
		RestartPolicy:      corev1.RestartPolicyOnFailure,
		SecurityContext:    &corev1.PodSecurityContext{FSGroup: &fsGroup},
		Volumes:            volumes,
		Containers: []corev1.Container{{
			Name:         containerName,
			Image:        e.entry.Image,
			Args:         []string{"daemon"},
			Env:          env,
			Ports:        []corev1.ContainerPort{{Name: "binding", ContainerPort: bindingPort}},
			VolumeMounts: mounts,
			ReadinessProbe: &corev1.Probe{
				ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstrPort(bindingPort)}},
				InitialDelaySeconds: 2,
				PeriodSeconds:       5,
			},
		}},
	}
}

func portAddr(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}
