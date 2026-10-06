package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	oauth2 "golang.org/x/oauth2"
	clientcredentials "golang.org/x/oauth2/clientcredentials"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"

	config "github.com/inference-gateway/cli/config"
)

// AuthError reports that a request to an agent could not be given credentials.
// Its reason names variables, URLs and status codes, never a secret.
type AuthError struct {
	Agent  string
	Reason string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("Authentication failed for A2A agent %q: %s", e.Agent, e.Reason)
}

// AuthFailure describes err for the model when it is an authentication
// failure: credentials that could not be obtained, or a 401 or 403 from the
// agent. The message names the agent and leaves the response body out.
func AuthFailure(agentURL string, err error) (string, bool) {
	if err == nil {
		return "", false
	}
	var authErr *AuthError
	if errors.As(err, &authErr) {
		return authErr.Error(), true
	}
	var status *adk.HTTPStatusError
	if !errors.As(err, &status) || (status.StatusCode != http.StatusUnauthorized && status.StatusCode != http.StatusForbidden) {
		return "", false
	}
	rejected := &AuthError{
		Agent:  agentDisplayName(agentURL),
		Reason: fmt.Sprintf("the agent rejected the request with status %d, check %s", status.StatusCode, credentialSetting(agentURL)),
	}
	return rejected.Error(), true
}

// credentialSetting names the setting that holds the credentials sent to agentURL.
func credentialSetting(agentURL string) string {
	if agent, ok := configuredAgent(agentURL); (!ok || agent.Auth == nil) && isGateway(agentURL) {
		return "gateway.api_key"
	}
	return "its auth settings in agents.yaml"
}

// PolicyRefusal describes err for the model when the gateway's guardrails
// refused the request: a 403 whose body is a JSON-RPC error. The message
// carries the policy's own words.
func PolicyRefusal(err error) (string, bool) {
	var status *adk.HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusForbidden {
		return "", false
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(status.Body), &envelope) != nil || envelope.Error.Message == "" {
		return "", false
	}
	return "The request was refused by a guardrail policy: " + envelope.Error.Message, true
}

// methodUnsupported describes err for the model when the agent does not
// implement the A2A method that was called, which no retry can change.
func methodUnsupported(agentURL string, err error) (string, bool) {
	if !errors.Is(err, adk.ErrMethodNotFound) {
		return "", false
	}
	return fmt.Sprintf("A2A agent %q does not implement the A2A method that was called, so it likely speaks another A2A protocol version than this CLI (v1.0). Retrying will not help.", agentDisplayName(agentURL)), true
}

// Rejection describes err for the model when the agent turned the request
// down for good, so retrying is pointless: a policy refusal, an
// authentication failure or a method the agent does not implement.
func Rejection(agentURL string, err error) (string, bool) {
	if message, ok := PolicyRefusal(err); ok {
		return message, true
	}
	if message, ok := methodUnsupported(agentURL, err); ok {
		return message, true
	}
	return AuthFailure(agentURL, err)
}

func agentDisplayName(agentURL string) string {
	if agent, ok := configuredAgent(agentURL); ok {
		return agent.Name
	}
	if u, err := url.Parse(agentURL); err == nil && u.Host != "" {
		return u.Host
	}
	return agentURL
}

func configuredAgent(agentURL string) (config.AgentEntry, bool) {
	agents, err := config.LoadAgents(config.ResolveAgentsPath())
	if err != nil {
		return config.AgentEntry{}, false
	}
	return agents.EntryForURL(agentURL)
}

// gatewayCredential is the bearer token the gateway expects, and the origin it belongs to.
// ponytail: one process-wide value set at startup. Pass it through NewClient
// if clients ever need different gateways.
var gatewayCredential struct {
	origin string
	token  string
}

// UseGatewayCredential makes apiKey the bearer token of A2A requests to the
// gateway at gatewayURL, the token the gateway also expects on inference.
func UseGatewayCredential(gatewayURL, apiKey string) {
	gatewayCredential.origin = config.URLOrigin(gatewayURL)
	gatewayCredential.token = apiKey
}

// authTransport adds an agent's credentials to the requests sent to its
// origin. Requests to any other origin, such as a redirect target, pass
// through untouched so a credential never leaves its agent.
type authTransport struct {
	base   http.RoundTripper
	origin string
	token  func() (string, error)
}

// newAuthTransport returns the transport for the agent at agentURL. It
// authenticates with the credentials agents.yaml gives the agent, or with the
// gateway credential when the agent is the gateway. Any other agent gets the default.
func newAuthTransport(agentURL string) http.RoundTripper {
	origin := config.URLOrigin(runningURL(agentURL))
	if agent, ok := configuredAgent(agentURL); ok && agent.Auth != nil {
		return authTransport{base: http.DefaultTransport, origin: origin, token: func() (string, error) { return bearerToken(agent) }}
	}
	if token := gatewayCredential.token; token != "" && isGateway(agentURL) {
		return authTransport{base: http.DefaultTransport, origin: origin, token: func() (string, error) { return token, nil }}
	}
	return http.DefaultTransport
}

func isGateway(agentURL string) bool {
	origin := config.URLOrigin(agentURL)
	return origin != "" && origin == gatewayCredential.origin
}

func (t authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if config.URLOrigin(req.URL.String()) != t.origin {
		return t.base.RoundTrip(req)
	}
	token, err := t.token()
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	authenticated := req.Clone(req.Context())
	authenticated.Header.Set("Authorization", "Bearer "+token)
	return t.base.RoundTrip(authenticated)
}

func bearerToken(agent config.AgentEntry) (string, error) {
	auth := agent.Auth
	sources := 0
	for _, set := range []bool{auth.TokenEnv != "", auth.TokenFile != "", len(auth.TokenCommand) > 0, auth.OIDC != nil} {
		if set {
			sources++
		}
	}
	switch {
	case sources > 1:
		return "", &AuthError{Agent: agent.Name, Reason: "auth sets more than one of token_env, token_file, token_command and oidc, keep one"}
	case auth.OIDC != nil:
		return oidcToken(agent)
	case auth.TokenEnv != "":
		return secretFromEnv(agent.Name, auth.TokenEnv)
	case auth.TokenFile != "":
		return secretFromFile(agent.Name, auth.TokenFile)
	case len(auth.TokenCommand) > 0:
		return commandToken(agent)
	default:
		return "", &AuthError{Agent: agent.Name, Reason: "auth sets none of token_env, token_file, token_command and oidc"}
	}
}

func secretFromEnv(agentName, variable string) (string, error) {
	if secret := os.Getenv(variable); secret != "" {
		return secret, nil
	}
	return "", &AuthError{Agent: agentName, Reason: fmt.Sprintf("environment variable %s is not set", variable)}
}

// secretFromFile reads the token on every call, so a token rotated in place by
// the kubelet, spiffe-helper or a secrets agent is picked up without a restart.
func secretFromFile(agentName, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", &AuthError{Agent: agentName, Reason: fmt.Sprintf("reading token_file %s: %v", path, err)}
	}
	if token := strings.TrimSpace(string(data)); token != "" {
		return token, nil
	}
	return "", &AuthError{Agent: agentName, Reason: fmt.Sprintf("token_file %s is empty", path)}
}

// credentialTimeout bounds one token fetch, whether from an issuer or a command.
const credentialTimeout = 10 * time.Second

// opaqueTokenTTL is how long a command's token is reused when it is not a JWT
// that states its own expiry.
// ponytail: flat 5 minute TTL for opaque tokens, add token_ttl if a provider needs it.
const opaqueTokenTTL = 5 * time.Minute

type commandCacheKey struct {
	agentURL string
	command  string
}

type cachedToken struct {
	token   string
	expires time.Time
}

// commandTokens keeps the last token each command produced until it expires,
// so a cloud CLI is not run on every request.
var commandTokens = struct {
	sync.Mutex
	byCommand map[commandCacheKey]cachedToken
}{byCommand: map[commandCacheKey]cachedToken{}}

// commandToken runs the agent's token_command and returns its stdout. The
// command is only honoured from the userspace agents.yaml, since a project file
// arrives with a clone and the agent card fetch runs without approval.
func commandToken(agent config.AgentEntry) (string, error) {
	if !agentsPathIsUserspace() {
		return "", &AuthError{Agent: agent.Name, Reason: "token_command is only read from ~/.infer/agents.yaml"}
	}
	key := commandCacheKey{agentURL: agent.URL, command: strings.Join(agent.Auth.TokenCommand, "\x00")}

	commandTokens.Lock()
	defer commandTokens.Unlock()

	if cached, ok := commandTokens.byCommand[key]; ok && time.Now().Before(cached.expires) {
		return cached.token, nil
	}
	token, err := runTokenCommand(agent.Name, agent.Auth.TokenCommand)
	if err != nil {
		return "", err
	}
	commandTokens.byCommand[key] = cachedToken{token: token, expires: tokenExpiry(token)}
	return token, nil
}

func runTokenCommand(agentName string, argv []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), credentialTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stderr = nil
	stdout, err := cmd.Output()
	if err != nil {
		return "", &AuthError{Agent: agentName, Reason: fmt.Sprintf("token_command %s failed: %s", argv[0], commandFailure(ctx, err))}
	}
	if token := strings.TrimSpace(string(stdout)); token != "" {
		return token, nil
	}
	return "", &AuthError{Agent: agentName, Reason: fmt.Sprintf("token_command %s printed no token", argv[0])}
}

// commandFailure names why a command failed without quoting its output, which
// may carry the token or another secret.
func commandFailure(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return fmt.Sprintf("timed out after %s", credentialTimeout)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return fmt.Sprintf("exit status %d", exit.ExitCode())
	}
	return err.Error()
}

// tokenExpiry is when a command's token stops being reused: shortly before the
// exp claim when the token is a JWT, otherwise after opaqueTokenTTL.
func tokenExpiry(token string) time.Time {
	fallback := time.Now().Add(opaqueTokenTTL)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fallback
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fallback
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return fallback
	}
	return time.Unix(claims.Exp, 0).Add(-30 * time.Second)
}

func agentsPathIsUserspace() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	userspace := filepath.Join(home, config.ConfigDirName, config.AgentsFileName)
	resolved, err := filepath.Abs(config.ResolveAgentsPath())
	return err == nil && resolved == userspace
}

var oidcHTTPClient = &http.Client{Timeout: credentialTimeout}

// oidcGrant identifies one client-credentials grant: a client at one agent.
type oidcGrant struct {
	agentURL string
	client   string
}

// oidcTokenSources keeps one token source per grant for the life of the
// process. Clients are built per request, so the cached token has to outlive them.
// ponytail: one lock also covers the first discovery of each grant. Give each
// grant its own sync.Once if agents ever wait on each other's issuers.
var oidcTokenSources = struct {
	sync.Mutex
	byGrant map[oidcGrant]oauth2.TokenSource
}{byGrant: map[oidcGrant]oauth2.TokenSource{}}

func oidcToken(agent config.AgentEntry) (string, error) {
	source, err := oidcTokenSource(agent)
	if err != nil {
		return "", err
	}
	token, err := source.Token()
	if err != nil {
		return "", &AuthError{Agent: agent.Name, Reason: "fetching the OIDC token: " + tokenFailureReason(err)}
	}
	return token.AccessToken, nil
}

// oidcTokenSource returns the token source of the agent's grant, which fetches
// a token once and refreshes it shortly before it expires. The token endpoint
// is the one the agent card declares.
func oidcTokenSource(agent config.AgentEntry) (oauth2.TokenSource, error) {
	oidc := *agent.Auth.OIDC
	grant := oidcGrant{agentURL: agent.URL, client: fmt.Sprintf("%+v", oidc)}

	oidcTokenSources.Lock()
	defer oidcTokenSources.Unlock()

	if source, ok := oidcTokenSources.byGrant[grant]; ok {
		return source, nil
	}
	secret, err := secretFromEnv(agent.Name, oidc.ClientSecretEnv)
	if err != nil {
		return nil, err
	}
	endpoint, err := declaredTokenEndpoint(agent.URL, oidc.IssuerURL)
	if err != nil {
		return nil, &AuthError{Agent: agent.Name, Reason: err.Error()}
	}
	credentials := clientcredentials.Config{
		ClientID:     oidc.ClientID,
		ClientSecret: secret,
		TokenURL:     endpoint.url,
		Scopes:       append(endpoint.scopes, oidc.Scopes...),
	}
	if oidc.Audience != "" {
		credentials.EndpointParams = url.Values{"audience": {oidc.Audience}}
	}
	source := credentials.TokenSource(context.WithValue(context.Background(), oauth2.HTTPClient, oidcHTTPClient))
	oidcTokenSources.byGrant[grant] = source
	return source, nil
}

// tokenEndpoint is where an agent's card says a client-credentials token is
// issued, with the scopes its security requirements ask for.
type tokenEndpoint struct {
	url    string
	scopes []string
}

// declaredTokenEndpoint reads the token endpoint from the security schemes of
// the agent's card: an openIdConnect scheme through its discovery document, or
// an oauth2 scheme's client-credentials flow. With a pinned issuer the card may
// only point there, so a tampered card cannot send the client secret elsewhere,
// and a card that declares no scheme falls back to that issuer.
func declaredTokenEndpoint(agentURL, pinnedIssuer string) (tokenEndpoint, error) {
	ctx, cancel := context.WithTimeout(context.Background(), credentialTimeout)
	defer cancel()

	card, err := client.NewClient(runningURL(agentURL)).GetAgentCard(ctx)
	if err != nil {
		return tokenEndpoint{}, fmt.Errorf("reading the security schemes of the agent card: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(card.SecuritySchemes)) {
		scheme := card.SecuritySchemes[name]
		endpoint := tokenEndpoint{scopes: requiredScopes(card, name)}
		switch {
		case scheme.OpenIDConnectSecurityScheme != nil:
			discoveryURL := scheme.OpenIDConnectSecurityScheme.OpenIDConnectURL
			if err := checkPinnedIssuer(discoveryURL, pinnedIssuer); err != nil {
				return tokenEndpoint{}, err
			}
			endpoint.url, err = discoverTokenEndpoint(discoveryURL)
			return endpoint, err
		case scheme.Oauth2SecurityScheme != nil && scheme.Oauth2SecurityScheme.Flows.ClientCredentials != nil:
			endpoint.url = scheme.Oauth2SecurityScheme.Flows.ClientCredentials.TokenURL
			return endpoint, checkPinnedIssuer(endpoint.url, pinnedIssuer)
		}
	}
	if pinnedIssuer == "" {
		return tokenEndpoint{}, errors.New("the agent card declares no openIdConnect or oauth2 client-credentials security scheme, set auth.oidc.issuer_url")
	}
	fallbackURL, err := discoverTokenEndpoint(strings.TrimSuffix(pinnedIssuer, "/") + "/.well-known/openid-configuration")
	return tokenEndpoint{url: fallbackURL}, err
}

func requiredScopes(card *adk.AgentCard, schemeName string) []string {
	for _, requirement := range card.SecurityRequirements {
		if scopes, ok := requirement.Schemes[schemeName]; ok {
			return scopes.List
		}
	}
	return nil
}

func checkPinnedIssuer(declaredURL, pinnedIssuer string) error {
	if pinnedIssuer == "" || strings.HasPrefix(declaredURL, strings.TrimSuffix(pinnedIssuer, "/")+"/") {
		return nil
	}
	return fmt.Errorf("the agent card points at %s, outside the pinned issuer_url %s", declaredURL, pinnedIssuer)
}

func discoverTokenEndpoint(discoveryURL string) (string, error) {
	resp, err := oidcHTTPClient.Get(discoveryURL)
	if err != nil {
		return "", fmt.Errorf("discovering the OIDC token endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discovering the OIDC token endpoint: the issuer answered with status %d", resp.StatusCode)
	}
	var discovery struct {
		TokenEndpoint string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return "", fmt.Errorf("reading the OIDC discovery document: %w", err)
	}
	if discovery.TokenEndpoint == "" {
		return "", errors.New("the OIDC discovery document has no token_endpoint")
	}
	return discovery.TokenEndpoint, nil
}

// tokenFailureReason describes a failed token request without the response
// body the oauth2 error carries.
func tokenFailureReason(err error) string {
	var rejected *oauth2.RetrieveError
	if errors.As(err, &rejected) && rejected.Response != nil {
		return strings.TrimSpace(fmt.Sprintf("the token endpoint answered with status %d %s", rejected.Response.StatusCode, rejected.ErrorCode))
	}
	return err.Error()
}
