package kubernetes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	k8sretry "k8s.io/client-go/util/retry"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// The operator Gateway a standard workspace deploys beside its sessions, and
// the gateway an all-in-one pod's workers run themselves.
const (
	gatewayKind       = "Gateway"
	gatewayResource   = "gateways"
	credentialsSuffix = "-credentials"
	revisionEnv       = "CREDENTIALS_REVISION"
	revisionKey       = "inference-gateway.com/credentials-revision"
	inPodGatewayURL   = "http://127.0.0.1:8080"
)

var gatewayGVR = k8sschema.GroupVersionResource{Group: orchestratorGroup, Version: orchestratorVersion, Resource: gatewayResource}

// gatewayName is the Gateway a workspace's sessions share. Workspaces sharing
// a namespace never share one.
func gatewayName(workspace string) string {
	sum := sha256.Sum256([]byte(workspace))
	return "infer-gateway-" + hex.EncodeToString(sum[:])[:12]
}

// orchestratorGateway is the spec.gateway the pod's workers call: inside the
// pod in all-in-one mode, else the workspace's own Gateway by ref, which the
// operator addresses and parks while every session is suspended. Nil leaves it
// to the image or the entry's env.
func (e *Workspace) orchestratorGateway() map[string]any {
	switch {
	case e.entry.Mode == config.WorkspaceModeAllInOne:
		return map[string]any{"url": inPodGatewayURL}
	case e.entry.DeploysGateway():
		return map[string]any{"ref": gatewayName(e.entry.Name)}
	}
	return nil
}

// ensureGateway writes gateway_env from this daemon's environment into the
// gateway's Secret and deploys the Gateway that reads it, both owned by every
// session's Orchestrator, so the last session to go takes them along.
func (e *Workspace) ensureGateway(ctx context.Context, orchestrator *k8sunstructured.Unstructured) error {
	revision, err := e.writeCredentials(ctx, orchestrator)
	if err != nil {
		return err
	}
	return e.applyGateway(ctx, orchestrator, revision)
}

// writeCredentials stores the credentials in the gateway's Secret and returns
// their revision, a counter on the Secret that only moves when the values do.
func (e *Workspace) writeCredentials(ctx context.Context, orchestrator *k8sunstructured.Unstructured) (string, error) {
	name := gatewayName(e.entry.Name) + credentialsSuffix
	var revision string
	err := k8sretry.OnError(k8sretry.DefaultRetry, raced, func() error {
		secret, err := e.upsertCredentials(ctx, name, orchestrator)
		if err == nil {
			revision = secret.Annotations[revisionKey]
		}
		return err
	})
	if err != nil {
		return "", fmt.Errorf("writing the gateway credentials Secret %s: %w", name, err)
	}
	return revision, nil
}

func (e *Workspace) upsertCredentials(ctx context.Context, name string, orchestrator *k8sunstructured.Unstructured) (*corev1.Secret, error) {
	secrets := e.typed.CoreV1().Secrets(e.entry.Namespace)
	secret, err := secrets.Get(ctx, name, metav1.GetOptions{})
	switch {
	case k8serrors.IsNotFound(err):
		return secrets.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: e.entry.Namespace, Labels: map[string]string{labelWorkspace: e.entry.Name},
				Annotations:     map[string]string{revisionKey: "1"},
				OwnerReferences: []metav1.OwnerReference{ownerOf(orchestrator)},
			},
			Data: e.credentials(nil),
		}, metav1.CreateOptions{})
	case err != nil:
		return nil, err
	}
	data, owners := e.credentials(secret.Data), addOwner(secret.OwnerReferences, orchestrator)
	changed := !maps.EqualFunc(data, secret.Data, bytes.Equal)
	if !changed && len(owners) == len(secret.OwnerReferences) {
		return secret, nil
	}
	if changed {
		revision, _ := strconv.Atoi(secret.Annotations[revisionKey])
		metav1.SetMetaDataAnnotation(&secret.ObjectMeta, revisionKey, strconv.Itoa(revision+1))
	}
	secret.Data, secret.OwnerReferences = data, owners
	return secrets.Update(ctx, secret, metav1.UpdateOptions{})
}

// credentials are the gateway_env values this daemon's environment or auth
// file sets. One neither sets keeps the stored value, so a daemon started
// without them leaves the keys an earlier teleport wrote.
func (e *Workspace) credentials(stored map[string][]byte) map[string][]byte {
	local, err := config.Credentials(e.entry.GatewayEnv)
	if err != nil {
		logger.Warn("the workspace gateway gets no keys from the auth file", "workspace", e.entry.Name, "error", err)
	}
	data := make(map[string][]byte, len(e.entry.GatewayEnv))
	for _, name := range e.entry.GatewayEnv {
		value, kept := stored[name]
		switch {
		case local[name] != "":
			data[name] = []byte(local[name])
		case kept:
			data[name] = value
		default:
			logger.Warn("the workspace gateway has no value for a gateway_env variable", "workspace", e.entry.Name, "variable", name)
		}
	}
	return data
}

// applyGateway creates the workspace's Gateway or brings its spec up to date,
// adding the session's Orchestrator to its owners.
func (e *Workspace) applyGateway(ctx context.Context, orchestrator *k8sunstructured.Unstructured, revision string) error {
	gateways := e.dynamic.Resource(gatewayGVR).Namespace(e.entry.Namespace)
	name := gatewayName(e.entry.Name)
	err := k8sretry.OnError(k8sretry.DefaultRetry, raced, func() error {
		gateway, err := gateways.Get(ctx, name, metav1.GetOptions{})
		missing := k8serrors.IsNotFound(err)
		if err != nil && !missing {
			return err
		}
		if missing {
			gateway = &k8sunstructured.Unstructured{Object: map[string]any{}}
			gateway.SetAPIVersion(orchestratorGroup + "/" + orchestratorVersion)
			gateway.SetKind(gatewayKind)
			gateway.SetName(name)
			gateway.SetNamespace(e.entry.Namespace)
			gateway.SetLabels(map[string]string{labelWorkspace: e.entry.Name})
		}
		gateway.SetOwnerReferences(addOwner(gateway.GetOwnerReferences(), orchestrator))
		gateway.Object["spec"] = e.gatewaySpec(revision)
		if missing {
			_, err = gateways.Create(ctx, gateway, metav1.CreateOptions{})
		} else {
			_, err = gateways.Update(ctx, gateway, metav1.UpdateOptions{})
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("deploying Gateway %s for workspace %s: %w", name, e.entry.Name, err)
	}
	logger.Info("the workspace gateway holds the session's provider credentials", "workspace", e.entry.Name, "gateway", name, "orchestrator", orchestrator.GetName())
	return nil
}

// awaitGateway polls until the workspace gateway has a pod that serves, since
// the restore already imports the conversation through it and a parked gateway
// only comes back with its session. The operator only deploys Gateways in
// namespaces its WATCH_NAMESPACE_SELECTOR matches.
// ponytail: one available pod is enough, so a rotated key may serve a turn from the old pod until its rollout ends.
func (e *Workspace) awaitGateway(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	ticker := time.NewTicker(readyPoll)
	defer ticker.Stop()
	name := gatewayName(e.entry.Name)
	deployed := false
	for {
		deployment, err := e.typed.AppsV1().Deployments(e.entry.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil && deployment.Status.AvailableReplicas > 0 {
			return nil
		}
		if err != nil && !k8serrors.IsNotFound(err) && ctx.Err() == nil {
			return fmt.Errorf("reading the Deployment of Gateway %s: %w", name, err)
		}
		deployed = deployed || err == nil
		select {
		case <-ctx.Done():
			if !deployed {
				return fmt.Errorf("the operator never deployed Gateway %s: label namespace %s to match its WATCH_NAMESPACE_SELECTOR, inference-gateway.com/managed=true by default", name, e.entry.Namespace)
			}
			return fmt.Errorf("gateway %s of workspace %s has no available pod in time", name, e.entry.Name)
		case <-ticker.C:
		}
	}
}

// gatewaySpec reads every gateway_env variable from the credentials Secret,
// grouped by provider, and serves on the operator's default port. The Secret's
// revision rides along, so a rotated key changes the spec and rolls the pods.
func (e *Workspace) gatewaySpec(revision string) map[string]any {
	secret := gatewayName(e.entry.Name) + credentialsSuffix
	byProvider := map[string][]any{}
	for _, name := range e.entry.GatewayEnv {
		provider := providerOf(name)
		byProvider[provider] = append(byProvider[provider], map[string]any{
			"name":      name,
			"valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": secret, "key": name, "optional": true}},
		})
	}
	providers := make([]any, 0, len(byProvider)+1)
	for _, provider := range slices.Sorted(maps.Keys(byProvider)) {
		providers = append(providers, map[string]any{"name": provider, "enabled": true, "env": byProvider[provider]})
	}
	providers = append(providers, map[string]any{"name": "credentials", "enabled": true, "env": []any{envVar(revisionEnv, revision)}})
	return map[string]any{"providers": providers}
}

// providerOf names the provider a gateway variable configures, the part
// before _API_: OLLAMA_CLOUD_API_KEY configures ollama_cloud.
func providerOf(variable string) string {
	provider, _, _ := strings.Cut(variable, "_API_")
	return strings.ToLower(provider)
}

func raced(err error) bool {
	return k8serrors.IsConflict(err) || k8serrors.IsAlreadyExists(err)
}
