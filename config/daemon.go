package config

import (
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const (
	DaemonFileName   = "daemon.yaml"
	DefaultDaemonDir = ConfigDirName + "/" + DaemonFileName

	defaultKubernetesIdleTTL = 2 * time.Hour
	defaultAwayExit          = time.Minute
	defaultMaxArtifactMB     = 1024
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

// WorkspaceProtocol is the machine-to-machine protocol a daemon talks to a
// workspace's daemon over: the AG-UI WebSocket binding, or A2A JSON-RPC.
type WorkspaceProtocol string

const (
	WorkspaceProtocolAGUI WorkspaceProtocol = "agui"
	WorkspaceProtocolA2A  WorkspaceProtocol = "a2a"
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
	AwayExit time.Duration `yaml:"away_exit,omitempty" mapstructure:"away_exit"`
	// MaxArtifactMB caps a session artifact a client reads from this daemon's
	// threads, sent whole in one frame. Zero or less takes the 1024 MB default.
	MaxArtifactMB int              `yaml:"max_artifact_mb,omitempty" mapstructure:"max_artifact_mb"`
	Binding       BindingConfig    `yaml:"binding" mapstructure:"binding"`
	Workspaces    []WorkspaceEntry `yaml:"workspaces" mapstructure:"workspaces"`
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
	Name string `yaml:"name" mapstructure:"name"`
	// Compute names the cluster or machine in compute.yaml the workspace runs
	// on, which then decides its kind and connection.
	Compute  string            `yaml:"compute,omitempty" mapstructure:"compute"`
	Kind     WorkspaceKind     `yaml:"kind,omitempty" mapstructure:"kind"`
	Protocol WorkspaceProtocol `yaml:"protocol,omitempty" mapstructure:"protocol"`
	Address  string            `yaml:"address,omitempty" mapstructure:"address"`
	Token    string            `yaml:"token,omitempty" mapstructure:"token"`
	TLS      PeerTLSConfig     `yaml:"tls,omitempty" mapstructure:"tls"`

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
	// ForwardVoiceSamples carries this machine's voice samples library with
	// every teleport to the workspace, so its sessions can clone the voices.
	ForwardVoiceSamples bool `yaml:"forward_voice_samples,omitempty" mapstructure:"forward_voice_samples"`

	// Mode is where a Kubernetes workspace runs the gateway.
	Mode WorkspaceMode `yaml:"mode,omitempty" mapstructure:"mode"`
	// GatewayEnv names the environment variables a teleport hands the gateway,
	// such as provider keys: its Secret in standard, the pod in all-in-one.
	GatewayEnv []string `yaml:"gateway_env,omitempty" mapstructure:"gateway_env"`
	// GatewayAudio is whether the gateway the workspace's sessions call must
	// serve its Audio API. ResolveCompute derives it from the environment of
	// every workspace that shares that gateway, so it is never written down.
	GatewayAudio bool `yaml:"-" mapstructure:"-"`
	// Secrets names Secrets in the namespace whose every key the pod daemon
	// gets as an environment variable, so each workspace holds its own
	// credentials and they never pass through this machine.
	Secrets []string `yaml:"secrets,omitempty" mapstructure:"secrets"`
	// MCPServers names hosted servers of this machine's mcp.yaml the pod
	// daemon gets, their headers as written so each stays a ${VAR} reference
	// the pod expands from its own Secrets.
	MCPServers []string `yaml:"mcp_servers,omitempty" mapstructure:"mcp_servers"`
	// UserData is a cloud-init user-data script the pod runs as root when it
	// boots, before the daemon starts, for example to install a browser.
	UserData string `yaml:"user_data,omitempty" mapstructure:"user_data"`
	// Volumes mounts PersistentVolumeClaims that already exist in the
	// namespace into the pod, for an app or content its sessions share.
	Volumes []WorkspaceVolume `yaml:"volumes,omitempty" mapstructure:"volumes"`
	// Resources are the CPU, memory and other resources a session's pod asks
	// the cluster for and may use at most. Empty leaves both to the cluster.
	Resources WorkspaceResources `yaml:"resources,omitempty" mapstructure:"resources"`
	// BindingService is metadata for the session's binding Service, for
	// controllers that act on Service metadata such as a VPN operator.
	BindingService BindingServiceConfig `yaml:"binding_service,omitempty" mapstructure:"binding_service"`
}

// WorkspaceVolume mounts one existing claim into a session's pod. The claim is
// created and filled elsewhere, and a session never creates or deletes it.
type WorkspaceVolume struct {
	Name      string `yaml:"name" mapstructure:"name"`
	Claim     string `yaml:"claim" mapstructure:"claim"`
	MountPath string `yaml:"mount_path" mapstructure:"mount_path"`
	ReadOnly  bool   `yaml:"read_only,omitempty" mapstructure:"read_only"`
}

// WorkspaceResources are a pod's resource requests and limits by resource
// name, such as cpu and memory, each a Kubernetes quantity.
type WorkspaceResources struct {
	Requests map[string]string `yaml:"requests,omitempty" mapstructure:"requests"`
	Limits   map[string]string `yaml:"limits,omitempty" mapstructure:"limits"`
}

// IsZero reports whether the workspace sets no request and no limit.
func (r WorkspaceResources) IsZero() bool {
	return len(r.Requests) == 0 && len(r.Limits) == 0
}

// BindingServiceConfig is copied verbatim onto each session's binding Service,
// with {{.Orchestrator}} expanded in the values.
type BindingServiceConfig struct {
	Annotations map[string]string `yaml:"annotations,omitempty" mapstructure:"annotations"`
	Labels      map[string]string `yaml:"labels,omitempty" mapstructure:"labels"`
}

func (s BindingServiceConfig) IsZero() bool {
	return len(s.Annotations) == 0 && len(s.Labels) == 0
}

// ForOrchestrator returns the entry with {{.Orchestrator}} expanded to the
// session's Orchestrator name in address and the binding Service metadata.
func (e WorkspaceEntry) ForOrchestrator(name string) (WorkspaceEntry, error) {
	var err error
	if e.Address, err = expandOrchestrator(e.Address, name); err != nil {
		return e, fmt.Errorf("address of workspace %q: %w", e.Name, err)
	}
	if e.BindingService.Annotations, err = expandOrchestratorValues(e.BindingService.Annotations, name); err != nil {
		return e, fmt.Errorf("binding_service.annotations of workspace %q: %w", e.Name, err)
	}
	if e.BindingService.Labels, err = expandOrchestratorValues(e.BindingService.Labels, name); err != nil {
		return e, fmt.Errorf("binding_service.labels of workspace %q: %w", e.Name, err)
	}
	return e, nil
}

func expandOrchestratorValues(values map[string]string, name string) (map[string]string, error) {
	expanded := maps.Clone(values)
	for key, value := range expanded {
		var err error
		if expanded[key], err = expandOrchestrator(value, name); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
	}
	return expanded, nil
}

func expandOrchestrator(value, name string) (string, error) {
	if !strings.Contains(value, "{{") {
		return value, nil
	}
	tmpl, err := template.New("").Option("missingkey=error").Parse(value)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if err := tmpl.Execute(&out, struct{ Orchestrator string }{name}); err != nil {
		return "", err
	}
	return out.String(), nil
}

// DeploysGateway reports whether the workspace runs a gateway of its own
// beside its sessions, which takes standard mode and variables to hand it.
func (e WorkspaceEntry) DeploysGateway() bool {
	return e.Mode != WorkspaceModeAllInOne && len(e.GatewayEnv) > 0
}

// EffectiveProtocol is the configured protocol, else the AG-UI WebSocket
// relay that empty keeps.
func (e WorkspaceEntry) EffectiveProtocol() WorkspaceProtocol {
	if e.Protocol == "" {
		return WorkspaceProtocolAGUI
	}
	return e.Protocol
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

// MaxArtifactBytes is MaxArtifactMB in bytes, the default when it is unset.
func (d DaemonConfig) MaxArtifactBytes() int64 {
	if d.MaxArtifactMB <= 0 {
		return defaultMaxArtifactMB << 20
	}
	return int64(d.MaxArtifactMB) << 20
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

// servesMutualTLS reports whether the binding serves TLS and admits only
// clients that present a verified certificate.
func (t BindingTLSConfig) servesMutualTLS() bool {
	return !t.Mode.off() && t.RequiresClientAuth()
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

// userDataScriptPrefix starts a cloud-init user-data script, the one user-data
// format a workspace runs today.
const userDataScriptPrefix = "#!"

// setsKubernetesOnlyFields reports whether the entry sets a field only a
// kubernetes workspace uses.
func (e WorkspaceEntry) setsKubernetesOnlyFields() bool {
	return e.Mode != "" || len(e.GatewayEnv) > 0 || len(e.Secrets) > 0 || len(e.MCPServers) > 0 ||
		!e.BindingService.IsZero() || e.UserData != "" || len(e.Volumes) > 0 || !e.Resources.IsZero()
}

// validateVolumes rejects a volume the pod could not mount: one without a
// name or a claim, a mount path that is not absolute, or a name used twice.
func (e WorkspaceEntry) validateVolumes() error {
	seen := map[string]bool{}
	for i, volume := range e.Volumes {
		switch {
		case volume.Name == "":
			return fmt.Errorf("workspace %q: volumes[%d] needs a name", e.Name, i)
		case seen[volume.Name]:
			return fmt.Errorf("workspace %q has two volumes named %q", e.Name, volume.Name)
		case volume.Claim == "":
			return fmt.Errorf("workspace %q: volume %q needs a claim, the PersistentVolumeClaim to mount", e.Name, volume.Name)
		case !strings.HasPrefix(volume.MountPath, "/"):
			return fmt.Errorf("workspace %q: volume %q needs an absolute mount_path, got %q", e.Name, volume.Name, volume.MountPath)
		}
		seen[volume.Name] = true
	}
	return nil
}

// validateResources rejects a request or a limit that names no resource or no
// quantity. Whether a value is a quantity the cluster takes is checked when a
// session's pod is built.
func (e WorkspaceEntry) validateResources() error {
	if err := e.validateAmounts("requests", e.Resources.Requests); err != nil {
		return err
	}
	return e.validateAmounts("limits", e.Resources.Limits)
}

func (e WorkspaceEntry) validateAmounts(kind string, amounts map[string]string) error {
	for _, name := range slices.Sorted(maps.Keys(amounts)) {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("workspace %q: resources.%s has an entry without a resource name", e.Name, kind)
		}
		if strings.TrimSpace(amounts[name]) == "" {
			return fmt.Errorf("workspace %q: resources.%s.%s needs a quantity", e.Name, kind, name)
		}
	}
	return nil
}

func (e WorkspaceEntry) validate(identity string) error {
	switch e.Protocol {
	case "", WorkspaceProtocolAGUI, WorkspaceProtocolA2A:
	default:
		return fmt.Errorf("invalid protocol %q for workspace %q: must be %q or %q", e.Protocol, e.Name, WorkspaceProtocolAGUI, WorkspaceProtocolA2A)
	}
	if err := e.Mode.validate(); err != nil {
		return fmt.Errorf("workspace %q: %w", e.Name, err)
	}
	if e.Kind != WorkspaceKindKubernetes && e.setsKubernetesOnlyFields() {
		return fmt.Errorf("workspace %q sets mode, gateway_env, secrets, mcp_servers, binding_service, user_data, volumes or resources, which only a kubernetes workspace uses", e.Name)
	}
	if e.UserData != "" && !strings.HasPrefix(e.UserData, userDataScriptPrefix) {
		return fmt.Errorf("workspace %q has user_data that is not a script: it must start with %q, as cloud-config is not supported yet", e.Name, userDataScriptPrefix)
	}
	if err := e.validateVolumes(); err != nil {
		return err
	}
	if err := e.validateResources(); err != nil {
		return err
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
		if _, err := e.ForOrchestrator("infer-000000000000"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid kind %q for workspace %q: must be one of %q, %q, or %q",
			e.Kind, e.Name, WorkspaceKindLocal, WorkspaceKindDaemon, WorkspaceKindKubernetes)
	}
	return nil
}

func (m WorkspaceMode) validate() error {
	switch m {
	case "", WorkspaceModeStandard, WorkspaceModeAllInOne:
		return nil
	}
	return fmt.Errorf("invalid mode %q: must be %q or %q", m, WorkspaceModeStandard, WorkspaceModeAllInOne)
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

// ExtensionLoopback resolves the plain loopback listener the opentask extension
// dials when the binding serves mutual TLS, since an extension cannot present a
// client certificate. It is disabled in every other setup, and an error when it
// would share the binding's port or has no token to authenticate the extension.
func (c *Config) ExtensionLoopback() (Binding, error) {
	binding := c.Binding()
	if !c.BrowserUse.extensionBackend() || !binding.TLS.servesMutualTLS() {
		return Binding{}, nil
	}
	extension := c.BrowserUse.Extension
	port := extension.EffectivePort()
	if port == binding.Port {
		return Binding{}, fmt.Errorf("browser_use.extension.port %d is also the AG-UI binding port - the binding requires client certificates, so set daemon.binding.port to another port and leave %d to the extension's loopback listener", port, port)
	}
	if extension.Token == "" {
		return Binding{}, fmt.Errorf("browser_use.extension.token is empty - the binding requires client certificates, so the extension's loopback listener on port %d needs the token to authenticate the extension", port)
	}
	return Binding{Enabled: true, Port: port, Token: extension.Token}, nil
}

// DefaultDaemonConfig returns the in-code defaults used when no daemon.yaml
// exists: the binding is off until the user or the desktop app enables it, no
// TLS, no workspaces beyond this daemon, and a minute's idle once every
// teleported session left.
func DefaultDaemonConfig() *DaemonConfig {
	return &DaemonConfig{
		AwayExit:      defaultAwayExit,
		MaxArtifactMB: defaultMaxArtifactMB,
		Binding: BindingConfig{
			TLS: BindingTLSConfig{Mode: TLSModeOff},
		},
		Workspaces: []WorkspaceEntry{},
	}
}
