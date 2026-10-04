package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"regexp"
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

// rejectedStatus matches the ADK client's error for a 401 or 403 response.
// ponytail: ADK has no typed HTTP error, so the status is read from the
// message. Switch to errors.As once ADK exposes the status code.
var rejectedStatus = regexp.MustCompile(`unexpected status code[^:]*: (401|403)\b`)

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
	match := rejectedStatus.FindStringSubmatch(err.Error())
	if match == nil {
		return "", false
	}
	rejected := &AuthError{
		Agent:  agentDisplayName(agentURL),
		Reason: fmt.Sprintf("the agent rejected the request with status %s, check its auth settings in agents.yaml", match[1]),
	}
	return rejected.Error(), true
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

// authTransport adds an agent's credentials to the requests sent to its
// origin. Requests to any other origin, such as a redirect target, pass
// through untouched so a credential never leaves its agent.
type authTransport struct {
	base   http.RoundTripper
	agent  config.AgentEntry
	origin string
}

// newAuthTransport returns the transport for the agent at agentURL: one that
// authenticates when agents.yaml gives the agent credentials, the default otherwise.
func newAuthTransport(agentURL string) http.RoundTripper {
	agent, ok := configuredAgent(agentURL)
	if !ok || agent.Auth == nil {
		return http.DefaultTransport
	}
	return authTransport{base: http.DefaultTransport, agent: agent, origin: config.URLOrigin(agentURL)}
}

func (t authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if config.URLOrigin(req.URL.String()) != t.origin {
		return t.base.RoundTrip(req)
	}
	token, err := bearerToken(t.agent)
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
	switch {
	case auth.TokenEnv != "" && auth.OIDC != nil:
		return "", &AuthError{Agent: agent.Name, Reason: "auth sets both token_env and oidc, keep one"}
	case auth.OIDC != nil:
		return oidcToken(agent)
	case auth.TokenEnv != "":
		return secretFromEnv(agent.Name, auth.TokenEnv)
	default:
		return "", &AuthError{Agent: agent.Name, Reason: "auth sets neither token_env nor oidc"}
	}
}

func secretFromEnv(agentName, variable string) (string, error) {
	if secret := os.Getenv(variable); secret != "" {
		return secret, nil
	}
	return "", &AuthError{Agent: agentName, Reason: fmt.Sprintf("environment variable %s is not set", variable)}
}

var oidcHTTPClient = &http.Client{Timeout: 10 * time.Second}

// oidcGrant identifies one client-credentials grant: a client at one agent.
type oidcGrant struct {
	agentURL string
	client   config.AgentOIDC
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
	grant := oidcGrant{agentURL: agent.URL, client: *agent.Auth.OIDC}

	oidcTokenSources.Lock()
	defer oidcTokenSources.Unlock()

	if source, ok := oidcTokenSources.byGrant[grant]; ok {
		return source, nil
	}
	secret, err := secretFromEnv(agent.Name, grant.client.ClientSecretEnv)
	if err != nil {
		return nil, err
	}
	endpoint, err := declaredTokenEndpoint(agent.URL, grant.client.IssuerURL)
	if err != nil {
		return nil, &AuthError{Agent: agent.Name, Reason: err.Error()}
	}
	credentials := clientcredentials.Config{
		ClientID:     grant.client.ClientID,
		ClientSecret: secret,
		TokenURL:     endpoint.url,
		Scopes:       endpoint.scopes,
	}
	if grant.client.Audience != "" {
		credentials.EndpointParams = url.Values{"audience": {grant.client.Audience}}
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
// only point there, so a tampered card cannot send the client secret elsewhere.
func declaredTokenEndpoint(agentURL, pinnedIssuer string) (tokenEndpoint, error) {
	ctx, cancel := context.WithTimeout(context.Background(), oidcHTTPClient.Timeout)
	defer cancel()

	card, err := client.NewClient(agentURL).GetAgentCard(ctx)
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
	return tokenEndpoint{}, errors.New("the agent card declares no openIdConnect or oauth2 client-credentials security scheme")
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
