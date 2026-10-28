package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DaemonFileName   = "daemon.yaml"
	DefaultDaemonDir = ConfigDirName + "/" + DaemonFileName

	// DefaultSPIFFESocket is where the SPIFFE CSI driver mounts the SPIRE
	// agent's Workload API inside a pod.
	DefaultSPIFFESocket = "unix:///spiffe-workload-api/spire-agent.sock"

	// WorkspacesDirName is the userspace directory a receiving daemon restores
	// transferred projects into.
	WorkspacesDirName = "workspaces"

	defaultKubernetesIdleTTL = 2 * time.Hour
	defaultDaemonLeaseTTL    = 24 * time.Hour
	remoteLeaseGrace         = 5 * time.Minute
)

// TLSMode is how the binding serves TLS: off, from certificate files, from
// the daemon-managed CA, or with SPIFFE SVIDs from the SPIRE Workload API.
type TLSMode string

const (
	TLSModeOff    TLSMode = "off"
	TLSModeFiles  TLSMode = "files"
	TLSModeCA     TLSMode = "ca"
	TLSModeSPIFFE TLSMode = "spiffe"
)

// ExecutorKind names where an agent runs: this daemon, a standalone daemon
// reachable over TLS, or a Kubernetes Sandbox per session.
type ExecutorKind string

const (
	ExecutorKindLocal      ExecutorKind = "local"
	ExecutorKindDaemon     ExecutorKind = "daemon"
	ExecutorKindKubernetes ExecutorKind = "kubernetes"
)

// ExecutorAuth is how a Kubernetes executor's pod daemon authenticates: a
// certificate the daemon-managed CA issues, or a SPIFFE SVID.
type ExecutorAuth string

const (
	ExecutorAuthCA     ExecutorAuth = "ca"
	ExecutorAuthSPIFFE ExecutorAuth = "spiffe"
)

// DaemonConfig contains settings for the `infer daemon` command itself, the
// long-running host of the channels, scheduler, heartbeat and the AG-UI
// binding, and the executors sessions can be transferred to. Stored in its own
// daemon.yaml sidecar.
type DaemonConfig struct {
	// Executor is this daemon's identity in session leases. Empty takes the
	// name of the local executor entry, else the hostname.
	Executor string `yaml:"executor,omitempty" mapstructure:"executor"`
	// WorkspaceRoot is where a receiving daemon restores transferred projects.
	WorkspaceRoot string `yaml:"workspace_root,omitempty" mapstructure:"workspace_root"`
	// IdleExit stops the daemon after this long with no threads and no clients.
	// Zero keeps it running.
	IdleExit  time.Duration   `yaml:"idle_exit,omitempty" mapstructure:"idle_exit"`
	Binding   BindingConfig   `yaml:"binding" mapstructure:"binding"`
	Executors []ExecutorEntry `yaml:"executors" mapstructure:"executors"`
}

// BindingConfig configures the AG-UI WebSocket binding the daemon hosts for
// the desktop app, the opentask extension, the channel clients and peer
// daemons. It serves on its own switch, host, port and token, and
// browser_use.extension's port and token stay the fallback.
type BindingConfig struct {
	// Enabled turns the binding on for the daemon, independent of
	// browser_use, so every client of the daemon's threads can reach it.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	// Host the binding listens on. Empty is loopback, and any other host
	// requires TLS.
	Host string `yaml:"host,omitempty" mapstructure:"host"`
	// Port the binding listens on. Empty falls back to
	// browser_use.extension.port.
	Port int `yaml:"port" mapstructure:"port"`
	// Token is the shared secret clients must present in their hello. Empty
	// falls back to browser_use.extension.token, and a binding that verifies
	// client certificates may run without one.
	Token string           `yaml:"token" mapstructure:"token"`
	TLS   BindingTLSConfig `yaml:"tls" mapstructure:"tls"`
}

// BindingTLSConfig is the TLS the binding serves, if any.
type BindingTLSConfig struct {
	Mode     TLSMode `yaml:"mode" mapstructure:"mode"`
	CertFile string  `yaml:"cert_file,omitempty" mapstructure:"cert_file"`
	KeyFile  string  `yaml:"key_file,omitempty" mapstructure:"key_file"`
	CAFile   string  `yaml:"ca_file,omitempty" mapstructure:"ca_file"`
	// ClientAuth requires and verifies client certificates against the CA.
	// The ca and spiffe modes always do.
	ClientAuth bool         `yaml:"client_auth" mapstructure:"client_auth"`
	SPIFFE     SPIFFEConfig `yaml:"spiffe" mapstructure:"spiffe"`
}

// SPIFFEConfig reaches the SPIRE Workload API for the binding's own SVID and
// names which peers it trusts.
type SPIFFEConfig struct {
	Socket      string `yaml:"socket,omitempty" mapstructure:"socket"`
	TrustDomain string `yaml:"trust_domain,omitempty" mapstructure:"trust_domain"`
	// AllowedIDs are the SPIFFE IDs accepted as peers. Empty accepts every
	// member of the trust domain.
	AllowedIDs []string `yaml:"allowed_ids,omitempty" mapstructure:"allowed_ids"`
}

// PeerTLSConfig is how this daemon reaches a daemon-kind executor over TLS:
// the CA that signed its certificate, and a client certificate when the
// peer requires one.
type PeerTLSConfig struct {
	CAFile   string `yaml:"ca_file,omitempty" mapstructure:"ca_file"`
	CertFile string `yaml:"cert_file,omitempty" mapstructure:"cert_file"`
	KeyFile  string `yaml:"key_file,omitempty" mapstructure:"key_file"`
}

// ExecutorSPIFFEConfig names the SVID a Kubernetes executor's pod daemon
// presents, so this daemon knows which identity to expect.
type ExecutorSPIFFEConfig struct {
	TrustDomain string `yaml:"trust_domain,omitempty" mapstructure:"trust_domain"`
	ServerID    string `yaml:"server_id,omitempty" mapstructure:"server_id"`
}

// ExecutorEntry registers one executor sessions can move to. The daemon kind
// uses Address, Token and TLS, the kubernetes kind the cluster fields, and
// Address overrides the port-forward when an Ingress exposes the pods.
type ExecutorEntry struct {
	Name    string        `yaml:"name" mapstructure:"name"`
	Kind    ExecutorKind  `yaml:"kind" mapstructure:"kind"`
	Address string        `yaml:"address,omitempty" mapstructure:"address"`
	Token   string        `yaml:"token,omitempty" mapstructure:"token"`
	TLS     PeerTLSConfig `yaml:"tls,omitempty" mapstructure:"tls"`

	Kubeconfig string               `yaml:"kubeconfig,omitempty" mapstructure:"kubeconfig"`
	Context    string               `yaml:"context,omitempty" mapstructure:"context"`
	Namespace  string               `yaml:"namespace,omitempty" mapstructure:"namespace"`
	Image      string               `yaml:"image,omitempty" mapstructure:"image"`
	Storage    string               `yaml:"storage,omitempty" mapstructure:"storage"`
	IdleTTL    time.Duration        `yaml:"idle_ttl,omitempty" mapstructure:"idle_ttl"`
	Auth       ExecutorAuth         `yaml:"auth,omitempty" mapstructure:"auth"`
	SPIFFE     ExecutorSPIFFEConfig `yaml:"spiffe,omitempty" mapstructure:"spiffe"`
	// Env is the non-secret environment the pod daemon starts with.
	Env map[string]string `yaml:"env,omitempty" mapstructure:"env"`

	// LeaseTTL is how long this daemon's lease row for a session the executor
	// owns stays live without contact. Empty derives it from the kind.
	LeaseTTL time.Duration `yaml:"lease_ttl,omitempty" mapstructure:"lease_ttl"`
	// ForwardEnv names the environment variables sealed into a handoff for
	// the target's workers, never written to the executor's own objects.
	ForwardEnv []string `yaml:"forward_env,omitempty" mapstructure:"forward_env"`
}

// EffectiveIdleTTL is how long a Kubernetes pod daemon idles before it
// suspends its Sandbox.
func (e ExecutorEntry) EffectiveIdleTTL() time.Duration {
	if e.IdleTTL > 0 {
		return e.IdleTTL
	}
	return defaultKubernetesIdleTTL
}

// EffectiveLeaseTTL is the configured lease TTL, else the idle TTL plus a
// grace for Kubernetes and a day for a standalone daemon.
func (e ExecutorEntry) EffectiveLeaseTTL() time.Duration {
	if e.LeaseTTL > 0 {
		return e.LeaseTTL
	}
	if e.Kind == ExecutorKindKubernetes {
		return e.EffectiveIdleTTL() + remoteLeaseGrace
	}
	return defaultDaemonLeaseTTL
}

// EffectiveAuth is the configured pod authentication, CA by default.
func (e ExecutorEntry) EffectiveAuth() ExecutorAuth {
	if e.Auth == "" {
		return ExecutorAuthCA
	}
	return e.Auth
}

// Identity is this daemon's name in session leases: the configured executor,
// else the local executor entry's name, else the hostname.
func (d DaemonConfig) Identity() string {
	if d.Executor != "" {
		return d.Executor
	}
	for _, executor := range d.Executors {
		if executor.Kind == ExecutorKindLocal {
			return executor.Name
		}
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return string(ExecutorKindLocal)
}

// WorkspaceDir is where a receiving daemon restores transferred projects.
func (d DaemonConfig) WorkspaceDir() string {
	if d.WorkspaceRoot == "" {
		return filepath.Join(UserSpaceConfigDir(), WorkspacesDirName)
	}
	if rest, ok := strings.CutPrefix(d.WorkspaceRoot, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return d.WorkspaceRoot
}

// Remote returns the executor entry of that name, false when it is unknown or
// the local one.
func (d DaemonConfig) Remote(name string) (ExecutorEntry, bool) {
	for _, executor := range d.Executors {
		if executor.Name == name && executor.Kind != ExecutorKindLocal {
			return executor, true
		}
	}
	return ExecutorEntry{}, false
}

// RequiresClientAuth reports whether the binding authenticates peers with
// client certificates, which the ca and spiffe modes always do.
func (t BindingTLSConfig) RequiresClientAuth() bool {
	return t.ClientAuth || t.Mode == TLSModeCA || t.Mode == TLSModeSPIFFE
}

// Validate checks daemon.yaml: the TLS mode, that a non-loopback binding
// serves TLS, and that every executor entry carries what its kind needs.
func (d DaemonConfig) Validate() error {
	if err := d.Binding.validate(); err != nil {
		return err
	}
	seen := make(map[string]bool, len(d.Executors))
	locals := 0
	for _, executor := range d.Executors {
		if executor.Name == "" {
			return fmt.Errorf("every executor needs a name")
		}
		if seen[executor.Name] {
			return fmt.Errorf("executor %q is registered twice", executor.Name)
		}
		seen[executor.Name] = true
		if executor.Kind == ExecutorKindLocal {
			locals++
		}
		if err := executor.validate(d.Executor); err != nil {
			return err
		}
	}
	if locals > 1 {
		return fmt.Errorf("only one executor may be of kind %q", ExecutorKindLocal)
	}
	return nil
}

func (b BindingConfig) validate() error {
	switch b.TLS.Mode {
	case "", TLSModeOff, TLSModeFiles, TLSModeCA, TLSModeSPIFFE:
	default:
		return fmt.Errorf("invalid binding.tls.mode %q: must be one of %q, %q, %q, or %q",
			b.TLS.Mode, TLSModeOff, TLSModeFiles, TLSModeCA, TLSModeSPIFFE)
	}
	if b.TLS.Mode == TLSModeFiles && (b.TLS.CertFile == "" || b.TLS.KeyFile == "") {
		return fmt.Errorf("binding.tls.mode %q needs cert_file and key_file", TLSModeFiles)
	}
	if b.TLS.Mode == TLSModeSPIFFE && b.TLS.SPIFFE.TrustDomain == "" {
		return fmt.Errorf("binding.tls.mode %q needs spiffe.trust_domain", TLSModeSPIFFE)
	}
	if !isLoopbackHost(b.Host) && b.TLS.Mode.off() {
		return fmt.Errorf("binding.host %q is not loopback, so binding.tls.mode must not be off", b.Host)
	}
	return nil
}

func (e ExecutorEntry) validate(identity string) error {
	switch e.Kind {
	case ExecutorKindLocal:
		if identity != "" && identity != e.Name {
			return fmt.Errorf("executor %q of kind local must be named after daemon.executor %q", e.Name, identity)
		}
	case ExecutorKindDaemon:
		if e.Address == "" {
			return fmt.Errorf("executor %q of kind daemon needs an address", e.Name)
		}
		if e.TLS.CAFile == "" {
			return fmt.Errorf("executor %q of kind daemon needs tls.ca_file, remote bindings require TLS", e.Name)
		}
	case ExecutorKindKubernetes:
		if e.Namespace == "" || e.Image == "" {
			return fmt.Errorf("executor %q of kind kubernetes needs a namespace and an image", e.Name)
		}
		switch e.EffectiveAuth() {
		case ExecutorAuthCA:
		case ExecutorAuthSPIFFE:
			if e.SPIFFE.TrustDomain == "" {
				return fmt.Errorf("executor %q with auth spiffe needs spiffe.trust_domain", e.Name)
			}
		default:
			return fmt.Errorf("invalid auth %q for executor %q: must be %q or %q", e.Auth, e.Name, ExecutorAuthCA, ExecutorAuthSPIFFE)
		}
	default:
		return fmt.Errorf("invalid kind %q for executor %q: must be one of %q, %q, or %q",
			e.Kind, e.Name, ExecutorKindLocal, ExecutorKindDaemon, ExecutorKindKubernetes)
	}
	return nil
}

func (m TLSMode) off() bool {
	return m == "" || m == TLSModeOff
}

func isLoopbackHost(host string) bool {
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Binding is the AG-UI binding as the config resolves it: daemon.yaml's own
// values, with browser_use.extension's port and token as the fallback, and
// on when the daemon switch or the extension backend needs it.
type Binding struct {
	Enabled bool
	Host    string
	Port    int
	Token   string
	TLS     BindingTLSConfig
}

// Addr is the address a client on this machine dials: loopback, whatever
// host the binding listens on.
func (b Binding) Addr() string {
	host := b.Host
	if host == "" || net.ParseIP(host) != nil && net.ParseIP(host).IsUnspecified() {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(b.Port))
}

// ListenHost is the host the binding listens on, loopback when unset.
func (b Binding) ListenHost() string {
	if b.Host == "" {
		return "127.0.0.1"
	}
	return b.Host
}

// Dialable reports whether a client can reach the binding: it is on and has
// a token to present.
func (b Binding) Dialable() bool {
	return b.Enabled && b.Token != ""
}

// Binding resolves the AG-UI binding from daemon.yaml and browser_use.yaml.
func (c *Config) Binding() Binding {
	b := Binding{
		Enabled: c.Daemon.Binding.Enabled || c.BrowserUse.Enabled && c.BrowserUse.Backend == BrowserBackendExtension,
		Host:    c.Daemon.Binding.Host,
		Port:    c.Daemon.Binding.Port,
		Token:   c.Daemon.Binding.Token,
		TLS:     c.Daemon.Binding.TLS,
	}
	if b.Port <= 0 {
		b.Port = c.BrowserUse.Extension.EffectivePort()
	}
	if b.Token == "" {
		b.Token = c.BrowserUse.Extension.Token
	}
	return b
}

// DefaultDaemonConfig returns the in-code defaults used when no daemon.yaml
// exists: the binding is off until the user or the desktop app enables it, no
// TLS, and no executors beyond this daemon.
func DefaultDaemonConfig() *DaemonConfig {
	return &DaemonConfig{
		Binding: BindingConfig{
			TLS: BindingTLSConfig{Mode: TLSModeOff, SPIFFE: SPIFFEConfig{Socket: DefaultSPIFFESocket}},
		},
		Executors: []ExecutorEntry{},
	}
}
