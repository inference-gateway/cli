package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	k8sdynamic "k8s.io/client-go/dynamic"
	k8srest "k8s.io/client-go/rest"
	k8sclientcmd "k8s.io/client-go/tools/clientcmd"

	config "github.com/inference-gateway/cli/config"
	computedomain "github.com/inference-gateway/cli/internal/compute/domain"
)

// The operator API a cluster has to serve for sessions to run in it, and the
// label that marks what runs on one cluster's compute.
const (
	OrchestratorGroup   = "core.inference-gateway.com"
	OrchestratorVersion = "v1alpha1"
	LabelCompute        = "inference-gateway.com/compute"

	// MinOperatorVersion is the first operator release that runs an
	// Orchestrator in a Sandbox from the image's own entrypoint and links it
	// to a Gateway by ref.
	MinOperatorVersion = "v0.29.0"
)

var OrchestratorGVR = k8sschema.GroupVersionResource{Group: OrchestratorGroup, Version: OrchestratorVersion, Resource: "orchestrators"}

// Clients reach one cluster: the REST config a port-forward dials, a dynamic
// client for resources, and a core client for the calls no resource client
// makes, the port-forward URL and the server version.
type Clients struct {
	Rest    *k8srest.Config
	Dynamic k8sdynamic.Interface
	Core    *k8srest.RESTClient
}

// Connect builds the clients for the cluster a kubeconfig and context name,
// the default kubeconfig and its current context when empty. what names the
// cluster's user in errors, such as "workspace code-reviewer".
func Connect(kubeconfig, kubeContext, what string) (Clients, error) {
	rules := k8sclientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	overrides := &k8sclientcmd.ConfigOverrides{CurrentContext: kubeContext}
	restConfig, err := k8sclientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return Clients{}, fmt.Errorf("loading the kubeconfig for %s: %w", what, err)
	}
	dynamic, err := k8sdynamic.NewForConfig(restConfig)
	if err != nil {
		return Clients{}, fmt.Errorf("building the Kubernetes client for %s: %w", what, err)
	}
	core, err := coreClientFor(restConfig)
	if err != nil {
		return Clients{}, fmt.Errorf("building the Kubernetes client for %s: %w", what, err)
	}
	return Clients{Rest: restConfig, Dynamic: dynamic, Core: core}, nil
}

// coreClientFor is a core/v1 REST client. Unlike the typed clientset it
// carries no scheme, which keeps every other API group out of the binary.
func coreClientFor(restConfig *k8srest.Config) (*k8srest.RESTClient, error) {
	coreConfig := k8sdynamic.ConfigFor(restConfig)
	coreConfig.APIPath = "/api"
	coreConfig.GroupVersion = &k8sschema.GroupVersion{Version: "v1"}
	return k8srest.RESTClientFor(coreConfig)
}

// RequireOperator fails unless the cluster serves Orchestrators in namespace,
// so nothing is created in a cluster without the operator. where names the
// cluster in errors. A cluster without the CRD answers the list with not found.
func RequireOperator(ctx context.Context, dynamic k8sdynamic.Interface, namespace, where string) error {
	_, err := dynamic.Resource(OrchestratorGVR).Namespace(namespace).List(ctx, metav1.ListOptions{Limit: 1})
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("looking for the Inference Gateway operator in %s: %w", where, err)
	}
	return fmt.Errorf("the Inference Gateway operator is not installed in %s: install %s or later from https://github.com/inference-gateway/operator/releases", where, MinOperatorVersion)
}

// ServerVersion is the cluster's Kubernetes version as it reports it.
func ServerVersion(ctx context.Context, core *k8srest.RESTClient) (string, error) {
	raw, err := core.Get().AbsPath("/version").DoRaw(ctx)
	if err != nil {
		return "", err
	}
	var version struct{ GitVersion string }
	return version.GitVersion, json.Unmarshal(raw, &version)
}

// Status checks one cluster: whether it is reachable and runs the operator,
// its Kubernetes version, and the sessions every workspace on it runs.
func Status(ctx context.Context, cluster config.ClusterEntry) computedomain.ClusterStatus {
	clients, err := Connect(cluster.Kubeconfig, cluster.Context, "cluster "+cluster.Name)
	if err != nil {
		return computedomain.ClusterStatus{Name: cluster.Name, Namespace: cluster.Namespace, Error: err.Error()}
	}
	return status(ctx, cluster, clients.Dynamic, func(ctx context.Context) (string, error) { return ServerVersion(ctx, clients.Core) })
}

func status(ctx context.Context, cluster config.ClusterEntry, dynamic k8sdynamic.Interface, version func(context.Context) (string, error)) computedomain.ClusterStatus {
	report := computedomain.ClusterStatus{Name: cluster.Name, Namespace: cluster.Namespace}
	if err := RequireOperator(ctx, dynamic, cluster.Namespace, "cluster "+cluster.Name); err != nil {
		report.Error = err.Error()
		return report
	}
	sessions, err := dynamic.Resource(OrchestratorGVR).Namespace(cluster.Namespace).List(ctx, metav1.ListOptions{LabelSelector: LabelCompute + "=" + cluster.Name})
	if err != nil {
		report.Error = fmt.Sprintf("listing the sessions of cluster %s: %v", cluster.Name, err)
		return report
	}
	report.Reachable, report.Sessions = true, len(sessions.Items)
	if v, err := version(ctx); err == nil {
		report.KubernetesVersion = v
	}
	return report
}
