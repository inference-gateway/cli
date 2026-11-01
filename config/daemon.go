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

	defaultKubernetesIdleTTL = 2 * time.Hour
	defaultAwayExit          = time.Minute
	defaultDaemonLeaseTTL    = 24 * time.Hour
	remoteLeaseGrace         = 5 * time.Minute
)

// TLSMode is how the binding serves TLS: off, from certificate files, or from
// the daemon-managed CA.
type TLSMode string

const (
	TLSModeOff   TLSMode = "off"
	TLSModeFiles TLSMode = "files"
	TLSModeCA    TLSMode = "ca"
)

// WorkspaceKind names where an agent runs: this daemon, a standalone daemon
// reachable over TLS, or a Kubernetes Sandbox per session.
type WorkspaceKind string

const (
	WorkspaceKindLocal      WorkspaceKind = "local"
	WorkspaceKindDaemon     WorkspaceKind = "daemon"
	WorkspaceKindKubernetes WorkspaceKind = "kubernetes"
)

// WorkspaceMode is where a Kubernetes workspace runs the gateway: standard
// deploys one beside the workspace so the provider keys never enter the
// agent's pod, all-in-one runs it inside the pod. Empty is standard.
type WorkspaceMode string

const (
	WorkspaceModeStandard WorkspaceMode = "standard"
	WorkspaceModeAllInOne WorkspaceMode = "all-in-one"
)

// DaemonConfig contains settings for the `infer daemon` command itself, the
// long-running host of the channels, scheduler, heartbeat and the AG-UI
// binding, and the workspaces sessions can be teleported to. Stored in its own
// daemon.yaml sidecar.
type DaemonConfig struct {
	// Workspace is this daemon's identity in session leases. Empty takes the
	// name of the local workspace entry, else the hostname.
	Workspace string `yaml:"workspace,omitempty" mapstructure:"workspace"`
	// ProjectsRoot is where a receiving daemon restores teleported projects.
	ProjectsRoot string `yaml:"projects_root,omitempty" mapstructure:"projects_root"`
	// IdleExit stops the daemon after this long with no threads and no clients.
	// Zero keeps it running.
	IdleExit time.Duration `yaml:"idle_exit,omitempty" mapstructure:"idle_exit"`
	// AwayExit stops the daemon sooner, after this long with no threads and no
	// clients once every session that teleported here left again. It only
	// shortens IdleExit, and zero leaves IdleExit alone.
	AwayExit   time.Duration    `yaml:"away_exit,omitempty" mapstructure:"away_exit"`
	Binding    BindingConfig    `yaml:"binding" mapstructure:"binding"`
	Workspaces []WorkspaceEntry `yaml:"workspaces" mapstructure:"workspaces"`
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
	Mode       TLSMode `yaml:"mode" mapstructure:"mode"`
	CertFile   string  `yaml:"cert_file,omitempty" mapstructure:"cert_file"`
	KeyFile    string  `yaml:"key_file,omitempty" mapstructure:"key_file"`
	CAFile     string  `yaml:"ca_file,omitempty" mapstructure:"ca_file"`
	ClientAuth bool    `yaml:"client_auth" mapstructure:"client_auth"`
}

// PeerTLSConfig is how infer reaches a TLS peer, a daemon-kind workspace or the
// gateway: the CA that signed its certificate and a client certificate when
// the peer requires one. A set CA replaces the system roots for a workspace and
// is added to them for the gateway.
type PeerTLSConfig struct {
	CAFile   string `yaml:"ca_file,omitempty" mapstructure:"ca_file"`
	CertFile string `yaml:"cert_file,omitempty" mapstructure:"cert_file"`
	KeyFile  string `yaml:"key_file,omitempty" mapstructure:"key_file"`
}

// WorkspaceEntry registers one workspace sessions can teleport to. The daemon kind
// uses Address, Token and TLS, the kubernetes kind the cluster fields, and
// Address overrides the port-forward when an Ingress exposes the pods.
type WorkspaceEntry struct {
	Name    string        `yaml:"name" mapstructure:"name"`
	Kind    WorkspaceKind `yaml:"kind" mapstructure:"kind"`
	Address string        `yaml:"address,omitempty" mapstructure:"address"`
	Token   string        `yaml:"token,omitempty" mapstructure:"token"`
	TLS     PeerTLSConfig `yaml:"tls,omitempty" mapstructure:"tls"`

	Kubeconfig string            `yaml:"kubeconfig,omitempty" mapstructure:"kubeconfig"`
	Context    string            `yaml:"context,omitempty" mapstructure:"context"`
	Namespace  string            `yaml:"namespace,omitempty" mapstructure:"namespace"`
	Image      string            `yaml:"image,omitempty" mapstructure:"image"`
	Storage    string            `yaml:"storage,omitempty" mapstructure:"storage"`
	IdleTTL    time.Duration     `yaml:"idle_ttl,omitempty" mapstructure:"idle_ttl"`
	Env        map[string]string `yaml:"env,omitempty" mapstructure:"env"`

	// AwayTTL is how long a Kubernetes pod daemon idles once its sessions all
	// teleported away before it suspends its Sandbox. Empty is a minute.
	AwayTTL time.Duration `yaml:"away_ttl,omitempty" mapstructure:"away_ttl"`

	// Retention is how long a Sandbox stays suspended once every agent on it is
	// done before agent-sandbox deletes it with its volumes. Zero keeps it until
	// the session is stopped.
	Retention time.Duration `yaml:"retention,omitempty" mapstructure:"retention"`
	// LeaseTTL is how long this daemon's lease row for a session the workspace
	// owns stays live without contact. Empty derives it from the kind.
	LeaseTTL time.Duration `yaml:"lease_ttl,omitempty" mapstructure:"lease_ttl"`
	// ForwardEnv names the environment variables sealed into a handoff for
	// the target's workers, never written to the workspace's own objects.
	ForwardEnv []string `yaml:"forward_env,omitempty" mapstructure:"forward_env"`

	// Mode is where a Kubernetes workspace runs the gateway.
	Mode WorkspaceMode `yaml:"mode,omitempty" mapstructure:"mode"`
	// GatewayEnv names the environment variables a teleport hands the gateway,
	// such as provider keys: its Secret in standard, the pod in all-in-one.
	GatewayEnv []string `yaml:"gateway_env,omitempty" mapstructure:"gateway_env"`
}

// DeploysGateway reports whether the workspace runs a gateway of its own
// beside its sessions, which takes standard mode and variables to hand it.
func (e WorkspaceEntry) DeploysGateway() bool {
	return e.Mode != WorkspaceModeAllInOne && len(e.GatewayEnv) > 0
}

// EffectiveIdleTTL is how long a Kubernetes pod daemon idles before it
// suspends its Sandbox.
func (e WorkspaceEntry) EffectiveIdleTTL() time.Duration {
	if e.IdleTTL > 0 {
		return e.IdleTTL
	}
	return defaultKubernetesIdleTTL
}

// EffectiveAwayTTL is how long a Kubernetes pod daemon idles once its sessions
// all teleported away before it suspends its Sandbox.
func (e WorkspaceEntry) EffectiveAwayTTL() time.Duration {
	if e.AwayTTL > 0 {
		return e.AwayTTL
	}
	return defaultAwayExit
}

// EffectiveLeaseTTL is the configured lease TTL, else the idle TTL plus a
// grace for Kubernetes and a day for a standalone daemon.
func (e WorkspaceEntry) EffectiveLeaseTTL() time.Duration {
	if e.LeaseTTL > 0 {
		return e.LeaseTTL
	}
	if e.Kind == WorkspaceKindKubernetes {
		return e.EffectiveIdleTTL() + remoteLeaseGrace
	}
	return defaultDaemonLeaseTTL
}

// Identity is this daemon's name in session leases: the configured workspace,
// else the local workspace entry's name, else the hostname.
func (d DaemonConfig) Identity() string {
	if d.Workspace != "" {
		return d.Workspace
	}
	for _, workspace := range d.Workspaces {
		if workspace.Kind == WorkspaceKindLocal {
			return workspace.Name
		}
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return string(WorkspaceKindLocal)
}

// ProjectsDir is where a receiving daemon restores teleported projects.
func (d DaemonConfig) ProjectsDir() string {
	if d.ProjectsRoot == "" {
		return filepath.Join(UserSpaceConfigDir(), ProjectsDirName)
	}
	if rest, ok := strings.CutPrefix(d.ProjectsRoot, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return d.ProjectsRoot
}

// Remote returns the workspace entry of that name, false when it is unknown or
// the local one.
func (d DaemonConfig) Remote(name string) (WorkspaceEntry, bool) {
	for _, workspace := range d.Workspaces {
		if workspace.Name == name && workspace.Kind != WorkspaceKindLocal {
			return workspace, true
		}
	}
	return WorkspaceEntry{}, false
}

// RequiresClientAuth reports whether the binding authenticates peers with
// client certificates, which the ca mode always does.
func (t BindingTLSConfig) RequiresClientAuth() bool {
	return t.ClientAuth || t.Mode == TLSModeCA
}

// Validate checks daemon.yaml: the TLS mode, that a non-loopback binding
// serves TLS, and that every workspace entry carries what its kind needs.
func (d DaemonConfig) Validate() error {
	if err := d.Binding.validate(); err != nil {
		return err
	}
	seen := make(map[string]bool, len(d.Workspaces))
	locals := 0
	for _, workspace := range d.Workspaces {
		if workspace.Name == "" {
			return fmt.Errorf("every workspace needs a name")
		}
		if seen[workspace.Name] {
			return fmt.Errorf("workspace %q is registered twice", workspace.Name)
		}
		seen[workspace.Name] = true
		if workspace.Kind == WorkspaceKindLocal {
			locals++
		}
		if err := workspace.validate(d.Workspace); err != nil {
			return err
		}
	}
	if locals > 1 {
		return fmt.Errorf("only one workspace may be of kind %q", WorkspaceKindLocal)
	}
	return nil
}

func (b BindingConfig) validate() error {
	switch b.TLS.Mode {
	case "", TLSModeOff, TLSModeFiles, TLSModeCA:
	default:
		return fmt.Errorf("invalid binding.tls.mode %q: must be one of %q, %q, or %q",
			b.TLS.Mode, TLSModeOff, TLSModeFiles, TLSModeCA)
	}
	if b.TLS.Mode == TLSModeFiles && (b.TLS.CertFile == "" || b.TLS.KeyFile == "") {
		return fmt.Errorf("binding.tls.mode %q needs cert_file and key_file", TLSModeFiles)
	}
	if !isLoopbackHost(b.Host) && b.TLS.Mode.off() {
		return fmt.Errorf("binding.host %q is not loopback, so binding.tls.mode must not be off", b.Host)
	}
	return nil
}

func (e WorkspaceEntry) validate(identity string) error {
	switch e.Mode {
	case "", WorkspaceModeStandard, WorkspaceModeAllInOne:
	default:
		return fmt.Errorf("invalid mode %q for workspace %q: must be %q or %q", e.Mode, e.Name, WorkspaceModeStandard, WorkspaceModeAllInOne)
	}
	if e.Kind != WorkspaceKindKubernetes && (e.Mode != "" || len(e.GatewayEnv) > 0) {
		return fmt.Errorf("workspace %q sets mode or gateway_env, which only a kubernetes workspace uses", e.Name)
	}
	switch e.Kind {
	case WorkspaceKindLocal:
		if identity != "" && identity != e.Name {
			return fmt.Errorf("workspace %q of kind local must be named after daemon.workspace %q", e.Name, identity)
		}
	case WorkspaceKindDaemon:
		if e.Address == "" {
			return fmt.Errorf("workspace %q of kind daemon needs an address", e.Name)
		}
	case WorkspaceKindKubernetes:
		if e.Namespace == "" || e.Image == "" {
			return fmt.Errorf("workspace %q of kind kubernetes needs a namespace and an image", e.Name)
		}
	default:
		return fmt.Errorf("invalid kind %q for workspace %q: must be one of %q, %q, or %q",
			e.Kind, e.Name, WorkspaceKindLocal, WorkspaceKindDaemon, WorkspaceKindKubernetes)
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

// Dialable reports whether a client can reach the binding: it is on and a
// token or a client certificate proves who the client is.
func (b Binding) Dialable() bool {
	return b.Enabled && (b.Token != "" || b.TLS.RequiresClientAuth())
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
// TLS, no workspaces beyond this daemon, and a minute's idle once every
// teleported session left.
func DefaultDaemonConfig() *DaemonConfig {
	return &DaemonConfig{
		AwayExit: defaultAwayExit,
		Binding: BindingConfig{
			TLS: BindingTLSConfig{Mode: TLSModeOff},
		},
		Workspaces: []WorkspaceEntry{},
	}
}
