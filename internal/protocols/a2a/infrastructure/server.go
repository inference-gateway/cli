package infrastructure

import (
	"context"
	"fmt"
	"strconv"
	"time"

	adkserver "github.com/inference-gateway/adk/server"
	serverconfig "github.com/inference-gateway/adk/server/config"
	adk "github.com/inference-gateway/adk/types"

	platformlogger "github.com/inference-gateway/cli/internal/platform/logger"
)

// Settings carries what the daemon passes over: the bind address, the port,
// the bearer token, the agent mode and time budget served tasks run with, and
// the card version.
type Settings struct {
	Host        string
	Port        int
	Token       string
	Mode        string
	TaskTimeout time.Duration
	Version     string
}

// Server is the A2A server the daemon hosts: the agent card plus the
// message/send, tasks/{get,list,cancel} methods, tasks kept in the ADK's
// in-memory store and run one at a time from its queue, each one a headless
// run.
type Server struct {
	inner adkserver.A2AServer
}

// NewServer builds the ADK A2A server around infer's values. The ADK reads no
// environment of its own: serverConfig.NewWithDefaults applies only its
// struct-tag defaults, so an unprefixed SERVER_PORT or AUTH_ENABLED in the
// user's shell is never read, and every knob is set from settings.
func NewServer(settings Settings) (*Server, error) {
	adkCfg, err := serverconfig.NewWithDefaults(context.Background(), &serverconfig.Config{
		AgentName:        "infer",
		AgentDescription: "The infer CLI: every task is one headless coding run and the final assistant message is the answer",
		AgentVersion:     settings.Version,
		ServerConfig: serverconfig.ServerConfig{
			Host: settings.Host,
			Port: strconv.Itoa(settings.Port),
		},
		AuthConfig: serverconfig.AuthConfig{
			Enabled: false,
			Token:   settings.Token,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("building the ADK server config: %w", err)
	}

	inner, err := adkserver.NewA2AServerBuilder(*adkCfg, platformlogger.GetGlobalLogger()).
		WithAgentCard(agentCard(interfaceURL(settings), settings.Version)).
		WithBackgroundTaskHandler(NewHeadlessTaskHandler(settings.Mode, settings.TaskTimeout)).
		Build()
	if err != nil {
		return nil, fmt.Errorf("building the A2A server: %w", err)
	}
	return &Server{inner: inner}, nil
}

// Start serves until the process stops. It blocks; run it in a goroutine.
func (s *Server) Start(ctx context.Context) error {
	return s.inner.Start(ctx)
}

// Stop shuts the HTTP server down and lets in-flight runs finish.
func (s *Server) Stop(ctx context.Context) error {
	return s.inner.Stop(ctx)
}

// interfaceURL is the card's supportedInterface URL: the address the server binds.
func interfaceURL(settings Settings) string {
	return fmt.Sprintf("http://%s:%d/", settings.Host, settings.Port)
}

// agentCard is the card infer serves: one generic task skill, text only, no
// streaming or push notifications, and the bearer scheme a caller needs.
func agentCard(url, version string) adk.AgentCard {
	schemes, requirements := adkserver.BearerTokenSecuritySchemes()
	return adk.AgentCard{
		Name:        "infer",
		Description: "The infer CLI: runs the task text as one headless coding run in this project and answers with the final assistant message",
		Version:     version,
		Capabilities: adk.AgentCapabilities{
			Streaming:         new(bool),
			PushNotifications: new(bool),
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		SupportedInterfaces: []adk.AgentInterface{{
			URL:             url,
			ProtocolBinding: "JSONRPC",
			ProtocolVersion: "0.3",
		}},
		Skills: []adk.AgentSkill{{
			ID:          "coding_task",
			Name:        "Run a coding task",
			Description: "Runs the task text as one infer headless run (plain prompt, no shortcuts, tools as configured locally) and returns the final assistant message",
			Tags:        []string{"coding", "agent", "infer"},
		}},
		SecuritySchemes:      schemes,
		SecurityRequirements: requirements,
	}
}
