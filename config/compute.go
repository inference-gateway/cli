package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	yaml "gopkg.in/yaml.v3"

	configutils "github.com/inference-gateway/cli/config/utils"
)

const (
	ComputeFileName    = "compute.yaml"
	DefaultComputePath = ConfigDirName + "/" + ComputeFileName

	// GPUProviderRunPod is the one provider GPUs are rented from today.
	GPUProviderRunPod = "runpod"
	// RunPodAPIKeyName is the auth.yaml key, or environment variable, holding
	// the RunPod API key the GPU commands manage instances with.
	RunPodAPIKeyName = "RUNPOD_API_KEY"
)

// ComputeConfig is what sessions and models run on, written down once and
// apart from any daemon that uses it: Kubernetes clusters, standalone
// machines, and how GPUs are rented. Stored in the userspace compute.yaml.
type ComputeConfig struct {
	Clusters []ClusterEntry `yaml:"clusters,omitempty" mapstructure:"clusters"`
	Machines []MachineEntry `yaml:"machines,omitempty" mapstructure:"machines"`
	GPUs     GPUConfig      `yaml:"gpus,omitempty" mapstructure:"gpus"`
}

// GPUConfig is how infer rents GPUs: the provider, what an instance serves by
// default, and the most an hour of one may cost. Zero values keep the
// commands' defaults, and a zero max_hourly allows any price.
type GPUConfig struct {
	Provider  string  `yaml:"provider,omitempty" mapstructure:"provider"`
	GPUType   string  `yaml:"gpu_type,omitempty" mapstructure:"gpu_type"`
	CloudType string  `yaml:"cloud_type,omitempty" mapstructure:"cloud_type"`
	Model     string  `yaml:"model,omitempty" mapstructure:"model"`
	Image     string  `yaml:"image,omitempty" mapstructure:"image"`
	DiskGB    int     `yaml:"disk_gb,omitempty" mapstructure:"disk_gb"`
	MaxHourly float64 `yaml:"max_hourly,omitempty" mapstructure:"max_hourly"`
}

// GPUSettings are the GPU settings in effect: compute.yaml's gpus, else the
// legacy provisioner block of config.yaml, which legacy reports so the
// caller can point the user at compute.yaml.
func (c *Config) GPUSettings() (settings GPUConfig, legacy bool) {
	if c.Compute.GPUs != (GPUConfig{}) {
		return c.Compute.GPUs, false
	}
	p := c.Provisioner
	settings = GPUConfig{Provider: p.Provider, GPUType: p.GPUType, CloudType: p.CloudType, Model: p.Model, Image: p.Image, DiskGB: p.DiskGB, MaxHourly: p.MaxHourly}
	return settings, settings != (GPUConfig{})
}

// UserComputePath is the userspace compute.yaml, the only one infer reads.
func UserComputePath() string {
	return filepath.Join(UserSpaceConfigDir(), ComputeFileName)
}

// LoadComputeFile reads compute.yaml as written, without expanding ${VAR}, so
// a command that edits it saves a reference back as a reference and never as
// the secret it names. A missing file is an empty one.
func LoadComputeFile(path string) (*ComputeConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &ComputeConfig{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read compute config: %w", err)
	}
	cfg := &ComputeConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse compute config: %w", err)
	}
	return cfg, nil
}

// SaveComputeFile writes compute.yaml after checking it.
func SaveComputeFile(path string, cfg *ComputeConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	return configutils.SaveYAML(path, "compute", cfg)
}

// ClusterEntry is a Kubernetes cluster sessions run in: how to reach it, the
// namespace every workspace on it shares, and the gateway they call.
type ClusterEntry struct {
	Name       string        `yaml:"name" mapstructure:"name"`
	Kubeconfig string        `yaml:"kubeconfig,omitempty" mapstructure:"kubeconfig"`
	Context    string        `yaml:"context,omitempty" mapstructure:"context"`
	Namespace  string        `yaml:"namespace" mapstructure:"namespace"`
	Mode       WorkspaceMode `yaml:"mode,omitempty" mapstructure:"mode"`
	GatewayEnv []string      `yaml:"gateway_env,omitempty" mapstructure:"gateway_env"`
}

// MachineEntry is a standalone infer daemon sessions run on, reached over TLS.
type MachineEntry struct {
	Name    string        `yaml:"name" mapstructure:"name"`
	Address string        `yaml:"address" mapstructure:"address"`
	Token   string        `yaml:"token,omitempty" mapstructure:"token"`
	TLS     PeerTLSConfig `yaml:"tls,omitempty" mapstructure:"tls"`
}

// Validate checks compute.yaml: every cluster and machine has a name no other
// one has, a cluster has a namespace and a valid mode, a machine an address.
func (c ComputeConfig) Validate() error {
	seen := map[string]bool{}
	name := func(kind, name string) error {
		if name == "" {
			return fmt.Errorf("every %s needs a name", kind)
		}
		if seen[name] {
			return fmt.Errorf("compute %q is defined twice", name)
		}
		seen[name] = true
		return nil
	}
	for _, cluster := range c.Clusters {
		if err := name("cluster", cluster.Name); err != nil {
			return err
		}
		if cluster.Namespace == "" {
			return fmt.Errorf("cluster %q needs a namespace", cluster.Name)
		}
		if err := cluster.Mode.validate(); err != nil {
			return fmt.Errorf("cluster %q: %w", cluster.Name, err)
		}
	}
	for _, machine := range c.Machines {
		if err := name("machine", machine.Name); err != nil {
			return err
		}
		if machine.Address == "" {
			return fmt.Errorf("machine %q needs an address", machine.Name)
		}
	}
	if c.GPUs.Provider != "" && c.GPUs.Provider != GPUProviderRunPod {
		return fmt.Errorf("invalid gpus.provider %q: must be %q", c.GPUs.Provider, GPUProviderRunPod)
	}
	return nil
}

// ResolveCompute fills every workspace that names its compute with that
// compute's kind and connection, so a cluster or machine is written down once
// however many workspaces run on it. A workspace may not repeat what its
// compute provides.
func (d *DaemonConfig) ResolveCompute(compute ComputeConfig) error {
	for i := range d.Workspaces {
		if err := d.Workspaces[i].resolveCompute(compute); err != nil {
			return err
		}
	}
	return nil
}

func (e *WorkspaceEntry) resolveCompute(compute ComputeConfig) error {
	if e.Compute == "" {
		return nil
	}
	if e.Kind != "" {
		return fmt.Errorf("workspace %q runs on compute %q, which decides its kind", e.Name, e.Compute)
	}
	if index := slices.IndexFunc(compute.Clusters, func(c ClusterEntry) bool { return c.Name == e.Compute }); index >= 0 {
		return e.runOnCluster(compute.Clusters[index])
	}
	if index := slices.IndexFunc(compute.Machines, func(m MachineEntry) bool { return m.Name == e.Compute }); index >= 0 {
		return e.runOnMachine(compute.Machines[index])
	}
	return fmt.Errorf("workspace %q runs on compute %q, which %s does not define", e.Name, e.Compute, ComputeFileName)
}

func (e *WorkspaceEntry) runOnCluster(cluster ClusterEntry) error {
	if e.Kubeconfig != "" || e.Context != "" || e.Namespace != "" || e.Mode != "" || len(e.GatewayEnv) > 0 {
		return fmt.Errorf("workspace %q runs on cluster %q, which sets its kubeconfig, context, namespace, mode and gateway_env", e.Name, cluster.Name)
	}
	e.Kind = WorkspaceKindKubernetes
	e.Kubeconfig, e.Context, e.Namespace = cluster.Kubeconfig, cluster.Context, cluster.Namespace
	e.Mode, e.GatewayEnv = cluster.Mode, slices.Clone(cluster.GatewayEnv)
	return nil
}

func (e *WorkspaceEntry) runOnMachine(machine MachineEntry) error {
	if e.Address != "" || e.Token != "" || e.TLS != (PeerTLSConfig{}) {
		return fmt.Errorf("workspace %q runs on machine %q, which sets its address, token and tls", e.Name, machine.Name)
	}
	e.Kind = WorkspaceKindDaemon
	e.Address, e.Token, e.TLS = machine.Address, machine.Token, machine.TLS
	return nil
}
