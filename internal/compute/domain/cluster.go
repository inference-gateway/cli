package domain

// ClusterStatus is what a check of one cluster found: whether it is reachable
// and runs the operator, its Kubernetes version, and how many sessions run on
// it across every workspace that names it.
type ClusterStatus struct {
	Name              string
	Namespace         string
	Reachable         bool
	Error             string
	KubernetesVersion string
	Sessions          int
}
