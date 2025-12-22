package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	handlers "github.com/inference-gateway/cli/internal/handlers"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	logger "github.com/inference-gateway/cli/internal/logger"
	svc "github.com/inference-gateway/cli/internal/services"
	history "github.com/inference-gateway/cli/internal/ui/history"
	utils "github.com/inference-gateway/cli/internal/utils"
	cobra "github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the API server for conversation storage queries",
	Long: `Start an HTTP API server that exposes REST endpoints for accessing conversation storage.
This allows the UI and other clients to query conversations, statistics, and manage stored data
without requiring direct database access.

The API server provides:
  - Conversation listing and querying
  - Conversation statistics and analytics
  - Metadata management
  - Health checks

All headless chat sessions continue to save their conversations directly to the configured
storage backend (JSONL, SQLite, PostgreSQL, or Redis).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := getConfigFromViper()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		port, _ := cmd.Flags().GetInt("port")
		host, _ := cmd.Flags().GetString("host")
		enableUI, _ := cmd.Flags().GetBool("ui")

		if port != 0 {
			cfg.API.Port = port
		}
		if host != "" {
			cfg.API.Host = host
		}

		return startAPIServer(cfg, V, enableUI)
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().Int("port", 0, "API server port (default: 8080)")
	serveCmd.Flags().String("host", "", "API server host (default: 127.0.0.1)")
	serveCmd.Flags().Bool("ui", false, "Start the web UI and automatically open it in browser")
}

func startAPIServer(cfg *config.Config, v any, enableUI bool) error {
	services, uiManager := initializeServices(cfg, enableUI)
	defer cleanupServices(services, uiManager)

	if err := checkStorageHealth(services.GetStorage()); err != nil {
		logger.Warn("Storage health check failed", "error", err)
		fmt.Printf("Warning: Storage backend may not be available: %v\n", err)
	}

	server, serverReady, serverErrors := setupAndStartServer(cfg, services)
	handleUIStartup(cfg, uiManager, serverReady, enableUI)

	return waitForShutdown(server, serverErrors)
}

func initializeServices(cfg *config.Config, enableUI bool) (*container.ServiceContainer, *svc.UIManager) {
	services := container.NewServiceContainer(cfg, V)

	var uiManager *svc.UIManager
	if enableUI {
		uiManager = svc.NewUIManager(cfg)
	}

	if agentManager := services.GetAgentManager(); agentManager != nil {
		ctx := context.Background()
		if err := agentManager.StartAgents(ctx); err != nil {
			logger.Error("Failed to start agents", "error", err)
		}
	}

	if mcpManager := services.GetMCPManager(); mcpManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		if err := mcpManager.StartServers(ctx); err != nil {
			logger.Error("Some MCP servers failed to start", "error", err)
		}
	}

	return services, uiManager
}

func cleanupServices(services *container.ServiceContainer, uiManager *svc.UIManager) {
	if uiManager != nil && uiManager.IsRunning() {
		_ = uiManager.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = services.Shutdown(ctx)
}

func checkStorageHealth(strg storage.ConversationStorage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return strg.Health(ctx)
}

func setupAndStartServer(cfg *config.Config, services *container.ServiceContainer) (*http.Server, chan struct{}, chan error) {
	historyManager, err := history.NewHistoryManager(1000)
	if err != nil {
		logger.Warn("Failed to initialize history manager", "error", err)
		historyManager = nil
	}

	apiHandler := handlers.NewAPIHandler(
		services.GetStorage(),
		services.GetModelService(),
		services.GetStateManager(),
		services.GetMCPManager(),
		historyManager,
	)

	sessionManager := handlers.NewSessionManager()
	wsHandler := handlers.NewWebSocketHandler(sessionManager)

	handler := setupHTTPHandlers(cfg, apiHandler, wsHandler)
	addr := fmt.Sprintf("%s:%d", cfg.API.Host, cfg.API.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  time.Duration(cfg.API.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.API.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.API.IdleTimeout) * time.Second,
	}

	serverErrors := make(chan error, 1)
	serverReady := make(chan struct{})

	go startServerAsync(server, addr, cfg, serverErrors, serverReady)

	return server, serverReady, serverErrors
}

func setupHTTPHandlers(cfg *config.Config, apiHandler *handlers.APIHandler, wsHandler *handlers.WebSocketHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", apiHandler.HandleHealth)
	mux.HandleFunc("/ws", wsHandler.HandleWebSocket)
	mux.HandleFunc("/api/v1/models", apiHandler.HandleListModels)
	mux.HandleFunc("/api/v1/conversations", apiHandler.HandleListConversations)
	mux.HandleFunc("/api/v1/conversations/", apiHandler.HandleConversationByID)
	mux.HandleFunc("/api/v1/conversations/needs-titles", apiHandler.HandleConversationsNeedingTitles)
	mux.HandleFunc("/api/v1/agents/status", apiHandler.HandleAgentsStatus)
	mux.HandleFunc("/api/v1/mcp/status", apiHandler.HandleMCPStatus)
	mux.HandleFunc("/api/history", apiHandler.HandleHistory)

	handler := http.Handler(mux)
	if cfg.API.CORS.Enabled {
		handler = enableCORS(handler, cfg.API.CORS)
	}

	return handler
}

func startServerAsync(server *http.Server, addr string, cfg *config.Config, serverErrors chan error, serverReady chan struct{}) {
	logger.Info("Starting API server", "address", addr)

	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	if waitForServerReady(addr) {
		printServerInfo(addr, cfg)
		close(serverReady)
	}
}

func waitForServerReady(addr string) bool {
	apiURL := fmt.Sprintf("http://%s/health", addr)
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(apiURL)
		if err == nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				logger.Warn("Failed to close response body", "error", closeErr)
			}
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
	}
	return false
}

func printServerInfo(addr string, cfg *config.Config) {
	fmt.Printf("API server listening on http://%s\n", addr)
	fmt.Printf("   Storage: %s\n", cfg.Storage.Type)
	fmt.Printf("\nAvailable endpoints:\n")
	fmt.Printf("   GET  /health                             - Health check\n")
	fmt.Printf("   WS   /ws                                 - WebSocket for live chat\n")
	fmt.Printf("   GET  /api/v1/models                      - List available models\n")
	fmt.Printf("   GET  /api/v1/conversations               - List conversations\n")
	fmt.Printf("   GET  /api/v1/conversations/:id           - Get conversation\n")
	fmt.Printf("   DELETE /api/v1/conversations/:id         - Delete conversation\n")
	fmt.Printf("   PATCH /api/v1/conversations/:id/metadata - Update metadata\n")
	fmt.Printf("   GET  /api/v1/conversations/needs-titles  - List conversations needing titles\n")
	fmt.Printf("   GET  /api/v1/agents/status               - Get A2A agents status\n")
	fmt.Printf("   GET  /api/v1/mcp/status                  - Get MCP servers status\n")
	fmt.Printf("   GET  /api/history                        - Get command history\n")
	fmt.Printf("   POST /api/history                        - Save command to history\n\n")
}

func handleUIStartup(cfg *config.Config, uiManager *svc.UIManager, serverReady chan struct{}, enableUI bool) {
	if !enableUI || uiManager == nil {
		return
	}

	go func() {
		<-serverReady

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		if err := uiManager.Start(ctx); err != nil {
			logger.Error("Failed to start UI server", "error", err)
			fmt.Printf("Warning: UI server failed to start: %v\n", err)
			return
		}

		if cfg.API.UI.AutoOpen {
			openBrowserForUI(uiManager)
		}
	}()
}

func openBrowserForUI(uiManager *svc.UIManager) {
	uiURL := uiManager.GetURL()
	logger.Info("Opening browser", "url", uiURL)
	if err := utils.OpenBrowser(uiURL); err != nil {
		logger.Warn("Failed to open browser", "error", err)
		fmt.Printf("Could not open browser automatically. Please visit %s\n", uiURL)
	} else {
		fmt.Printf("Browser opened at %s\n", uiURL)
	}
}

func waitForShutdown(server *http.Server, serverErrors chan error) error {
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		return fmt.Errorf("server error: %w", err)
	case sig := <-shutdown:
		logger.Info("Shutting down API server", "signal", sig)
		fmt.Printf("\nShutting down gracefully...\n")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			if closeErr := server.Close(); closeErr != nil {
				logger.Warn("Failed to force close server", "error", closeErr)
			}
			return fmt.Errorf("could not stop server gracefully: %w", err)
		}
	}

	return nil
}

// enableCORS wraps the handler with CORS middleware
func enableCORS(next http.Handler, corsConfig config.CORSConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		allowed := false
		for _, allowedOrigin := range corsConfig.AllowedOrigins {
			if allowedOrigin == "*" || allowedOrigin == origin {
				allowed = true
				break
			}
		}

		if allowed {
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else if len(corsConfig.AllowedOrigins) > 0 {
				w.Header().Set("Access-Control-Allow-Origin", corsConfig.AllowedOrigins[0])
			}
			w.Header().Set("Access-Control-Allow-Methods", joinStrings(corsConfig.AllowedMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", joinStrings(corsConfig.AllowedHeaders, ", "))
			w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// joinStrings joins a slice of strings with a separator
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for _, s := range strs[1:] {
		result += sep + s
	}
	return result
}
