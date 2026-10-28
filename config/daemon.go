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

// EnvironmentKind names where an agent runs: this daemon, a standalone daemon
// reachable over TLS, or a Kubernetes Sandbox per session.
type EnvironmentKind string

const (
	EnvironmentKindLocal      EnvironmentKind = "local"
	EnvironmentKindDaemon     EnvironmentKind = "daemon"
	EnvironmentKindKubernetes EnvironmentKind = "kubernetes"
)

// EnvironmentAuth is how a Kubernetes environment's pod daemon authenticates: a
// certificate the daemon-managed CA issues, or a SPIFFE SVID.
type EnvironmentAuth string

const (
	EnvironmentAuthCA     EnvironmentAuth = "ca"
	EnvironmentAuthSPIFFE EnvironmentAuth = "spiffe"
)

// DaemonConfig contains settings for the `infer daemon` command itself, the
// long-running host of the channels, scheduler, heartbeat and the AG-UI
// binding, and the environments sessions can be transferred to. Stored in its own
// daemon.yaml sidecar.
type DaemonConfig struct {
	// Environment is this daemon's identity in session leases. Empty takes the
	// name of the local environment entry, else the hostname.
	Environment string `yaml:"environment,omitempty" mapstructure:"environment"`
	// WorkspaceRoot is where a receiving daemon restores transferred projects.
	WorkspaceRoot string `yaml:"workspace_root,omitempty" mapstructure:"workspace_root"`
	// IdleExit stops the daemon after this long with no threads and no clients.
	// Zero keeps it running.
	IdleExit     time.Duration      `yaml:"idle_exit,omitempty" mapstructure:"idle_exit"`
	Binding      BindingConfig      `yaml:"binding" mapstructure:"binding"`
	Environments []EnvironmentEntry `yaml:"environments" mapstructure:"environments"`
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
	Mode       TLSMode      `yaml:"mode" mapstructure:"mode"`
	CertFile   string       `yaml:"cert_file,omitempty" mapstructure:"cert_file"`
	KeyFile    string       `yaml:"key_file,omitempty" mapstructure:"key_file"`
	CAFile     string       `yaml:"ca_file,omitempty" mapstructure:"ca_file"`
	ClientAuth bool         `yaml:"client_auth" mapstructure:"client_auth"`
	SPIFFE     SPIFFEConfig `yaml:"spiffe" mapstructure:"spiffe"`
}

// SPIFFEConfig reaches the SPIRE Workload API for the binding's own SVID and
// names which peers it trusts.
type SPIFFEConfig struct {
	Socket      string   `yaml:"socket,omitempty" mapstructure:"socket"`
	TrustDomain string   `yaml:"trust_domain,omitempty" mapstructure:"trust_domain"`
	AllowedIDs  []string `yaml:"allowed_ids,omitempty" mapstructure:"allowed_ids"`
}

// PeerTLSConfig is how this daemon reaches a daemon-kind environment over TLS:
// the CA that signed its certificate, and a client certificate when the
// peer requires one.
type PeerTLSConfig struct {
	CAFile   string `yaml:"ca_file,omitempty" mapstructure:"ca_file"`
	CertFile string `yaml:"cert_file,omitempty" mapstructure:"cert_file"`
	KeyFile  string `yaml:"key_file,omitempty" mapstructure:"key_file"`
}

// EnvironmentSPIFFEConfig names the SVID a Kubernetes environment's pod daemon
// presents, so this daemon knows which identity to expect.
type EnvironmentSPIFFEConfig struct {
	TrustDomain string `yaml:"trust_domain,omitempty" mapstructure:"trust_domain"`
	ServerID    string `yaml:"server_id,omitempty" mapstructure:"server_id"`
}

// EnvironmentEntry registers one environment sessions can move to. The daemon kind
// uses Address, Token and TLS, the kubernetes kind the cluster fields, and
// Address overrides the port-forward when an Ingress exposes the pods.
type EnvironmentEntry struct {
	Name    string          `yaml:"name" mapstructure:"name"`
	Kind    EnvironmentKind `yaml:"kind" mapstructure:"kind"`
	Address string          `yaml:"address,omitempty" mapstructure:"address"`
	Token   string          `yaml:"token,omitempty" mapstructure:"token"`
	TLS     PeerTLSConfig   `yaml:"tls,omitempty" mapstructure:"tls"`

	Kubeconfig string                  `yaml:"kubeconfig,omitempty" mapstructure:"kubeconfig"`
	Context    string                  `yaml:"context,omitempty" mapstructure:"context"`
	Namespace  string                  `yaml:"namespace,omitempty" mapstructure:"namespace"`
	Image      string                  `yaml:"image,omitempty" mapstructure:"image"`
	Storage    string                  `yaml:"storage,omitempty" mapstructure:"storage"`
	IdleTTL    time.Duration           `yaml:"idle_ttl,omitempty" mapstructure:"idle_ttl"`
	Auth       EnvironmentAuth         `yaml:"auth,omitempty" mapstructure:"auth"`
	SPIFFE     EnvironmentSPIFFEConfig `yaml:"spiffe,omitempty" mapstructure:"spiffe"`
	Env        map[string]string       `yaml:"env,omitempty" mapstructure:"env"`

	// Retention is how long a Sandbox stays suspended once every agent on it is
	// done before agent-sandbox deletes it with its volumes. Zero keeps it until
	// the session is stopped.
	Retention time.Duration `yaml:"retention,omitempty" mapstructure:"retention"`
	// LeaseTTL is how long this daemon's lease row for a session the environment
	// owns stays live without contact. Empty derives it from the kind.
	LeaseTTL time.Duration `yaml:"lease_ttl,omitempty" mapstructure:"lease_ttl"`
	// ForwardEnv names the environment variables sealed into a handoff for
	// the target's workers, never written to the environment's own objects.
	ForwardEnv []string `yaml:"forward_env,omitempty" mapstructure:"forward_env"`
}

// EffectiveIdleTTL is how long a Kubernetes pod daemon idles before it
// suspends its Sandbox.
func (e EnvironmentEntry) EffectiveIdleTTL() time.Duration {
	if e.IdleTTL > 0 {
		return e.IdleTTL
	}
	return defaultKubernetesIdleTTL
}

// EffectiveLeaseTTL is the configured lease TTL, else the idle TTL plus a
// grace for Kubernetes and a day for a standalone daemon.
func (e EnvironmentEntry) EffectiveLeaseTTL() time.Duration {
	if e.LeaseTTL > 0 {
		return e.LeaseTTL
	}
	if e.Kind == EnvironmentKindKubernetes {
		return e.EffectiveIdleTTL() + remoteLeaseGrace
	}
	return defaultDaemonLeaseTTL
}

// EffectiveAuth is the configured pod authentication, CA by default.
func (e EnvironmentEntry) EffectiveAuth() EnvironmentAuth {
	if e.Auth == "" {
		return EnvironmentAuthCA
	}
	return e.Auth
}

// Identity is this daemon's name in session leases: the configured environment,
// else the local environment entry's name, else the hostname.
func (d DaemonConfig) Identity() string {
	if d.Environment != "" {
		return d.Environment
	}
	for _, environment := range d.Environments {
		if environment.Kind == EnvironmentKindLocal {
			return environment.Name
		}
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return string(EnvironmentKindLocal)
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

// Remote returns the environment entry of that name, false when it is unknown or
// the local one.
func (d DaemonConfig) Remote(name string) (EnvironmentEntry, bool) {
	for _, environment := range d.Environments {
		if environment.Name == name && environment.Kind != EnvironmentKindLocal {
			return environment, true
		}
	}
	return EnvironmentEntry{}, false
}

// RequiresClientAuth reports whether the binding authenticates peers with
// client certificates, which the ca and spiffe modes always do.
func (t BindingTLSConfig) RequiresClientAuth() bool {
	return t.ClientAuth || t.Mode == TLSModeCA || t.Mode == TLSModeSPIFFE
}

// Validate checks daemon.yaml: the TLS mode, that a non-loopback binding
// serves TLS, and that every environment entry carries what its kind needs.
func (d DaemonConfig) Validate() error {
	if err := d.Binding.validate(); err != nil {
		return err
	}
	seen := make(map[string]bool, len(d.Environments))
	locals := 0
	for _, environment := range d.Environments {
		if environment.Name == "" {
			return fmt.Errorf("every environment needs a name")
		}
		if seen[environment.Name] {
			return fmt.Errorf("environment %q is registered twice", environment.Name)
		}
		seen[environment.Name] = true
		if environment.Kind == EnvironmentKindLocal {
			locals++
		}
		if err := environment.validate(d.Environment); err != nil {
			return err
		}
	}
	if locals > 1 {
		return fmt.Errorf("only one environment may be of kind %q", EnvironmentKindLocal)
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

func (e EnvironmentEntry) validate(identity string) error {
	switch e.Kind {
	case EnvironmentKindLocal:
		if identity != "" && identity != e.Name {
			return fmt.Errorf("environment %q of kind local must be named after daemon.environment %q", e.Name, identity)
		}
	case EnvironmentKindDaemon:
		if e.Address == "" {
			return fmt.Errorf("environment %q of kind daemon needs an address", e.Name)
		}
		if e.TLS.CAFile == "" {
			return fmt.Errorf("environment %q of kind daemon needs tls.ca_file, remote bindings require TLS", e.Name)
		}
	case EnvironmentKindKubernetes:
		if e.Namespace == "" || e.Image == "" {
			return fmt.Errorf("environment %q of kind kubernetes needs a namespace and an image", e.Name)
		}
		switch e.EffectiveAuth() {
		case EnvironmentAuthCA:
		case EnvironmentAuthSPIFFE:
			if e.SPIFFE.TrustDomain == "" {
				return fmt.Errorf("environment %q with auth spiffe needs spiffe.trust_domain", e.Name)
			}
		default:
			return fmt.Errorf("invalid auth %q for environment %q: must be %q or %q", e.Auth, e.Name, EnvironmentAuthCA, EnvironmentAuthSPIFFE)
		}
	default:
		return fmt.Errorf("invalid kind %q for environment %q: must be one of %q, %q, or %q",
			e.Kind, e.Name, EnvironmentKindLocal, EnvironmentKindDaemon, EnvironmentKindKubernetes)
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
// TLS, and no environments beyond this daemon.
func DefaultDaemonConfig() *DaemonConfig {
	return &DaemonConfig{
		Binding: BindingConfig{
			TLS: BindingTLSConfig{Mode: TLSModeOff, SPIFFE: SPIFFEConfig{Socket: DefaultSPIFFESocket}},
		},
		Environments: []EnvironmentEntry{},
	}
}
