package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	logger "github.com/inference-gateway/cli/internal/logger"
	history "github.com/inference-gateway/cli/internal/ui/history"
)

// APIHandler handles HTTP API requests for conversation storage
type APIHandler struct {
	storage        storage.ConversationStorage
	modelService   domain.ModelService
	stateManager   domain.StateManager
	mcpManager     domain.MCPManager
	historyManager *history.HistoryManager
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(
	storage storage.ConversationStorage,
	modelService domain.ModelService,
	stateManager domain.StateManager,
	mcpManager domain.MCPManager,
	historyManager *history.HistoryManager,
) *APIHandler {
	return &APIHandler{
		storage:        storage,
		modelService:   modelService,
		stateManager:   stateManager,
		mcpManager:     mcpManager,
		historyManager: historyManager,
	}
}

// writeJSON writes a JSON response and logs errors
func (h *APIHandler) writeJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Error("Failed to encode JSON response", "error", err)
	}
}

// HandleHealth handles health check requests
func (h *APIHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	err := h.storage.Health(ctx)
	if err != nil {
		logger.Error("Storage health check failed", "error", err)
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unhealthy",
			"error":  err.Error(),
			"time":   time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"status": "healthy",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// HandleListConversations handles GET /api/v1/conversations
func (h *APIHandler) HandleListConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 50
	offset := 0

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	conversations, err := h.storage.ListConversations(ctx, limit, offset)
	if err != nil {
		logger.Error("Failed to list conversations", "error", err)
		h.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to list conversations: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"conversations": conversations,
		"count":         len(conversations),
		"limit":         limit,
		"offset":        offset,
	})
}

// HandleConversationByID handles conversation-specific operations
// GET /api/v1/conversations/:id - Get conversation
// DELETE /api/v1/conversations/:id - Delete conversation
// PATCH /api/v1/conversations/:id/metadata - Update metadata
func (h *APIHandler) HandleConversationByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/conversations/")
	if path == "" {
		http.Error(w, "Conversation ID required", http.StatusBadRequest)
		return
	}

	parts := strings.Split(path, "/")
	conversationID := parts[0]

	if len(parts) > 1 && parts[1] == "metadata" {
		if r.Method == http.MethodPatch {
			h.handleUpdateMetadata(w, r, conversationID)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleGetConversation(w, r, conversationID)
	case http.MethodDelete:
		h.handleDeleteConversation(w, r, conversationID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetConversation retrieves a specific conversation
func (h *APIHandler) handleGetConversation(w http.ResponseWriter, r *http.Request, conversationID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	entries, metadata, err := h.storage.LoadConversation(ctx, conversationID)
	if err != nil {
		logger.Error("Failed to load conversation", "id", conversationID, "error", err)
		h.writeJSON(w, http.StatusNotFound, map[string]any{
			"error": fmt.Sprintf("Conversation not found: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"id":       conversationID,
		"entries":  entries,
		"metadata": metadata,
	})
}

// handleDeleteConversation deletes a conversation
func (h *APIHandler) handleDeleteConversation(w http.ResponseWriter, r *http.Request, conversationID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	err := h.storage.DeleteConversation(ctx, conversationID)
	if err != nil {
		logger.Error("Failed to delete conversation", "id", conversationID, "error", err)
		h.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to delete conversation: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Conversation deleted successfully",
	})
}

// handleUpdateMetadata updates conversation metadata
func (h *APIHandler) handleUpdateMetadata(w http.ResponseWriter, r *http.Request, conversationID string) {
	var updates storage.ConversationMetadata
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	updates.ID = conversationID

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	err := h.storage.UpdateConversationMetadata(ctx, conversationID, updates)
	if err != nil {
		logger.Error("Failed to update conversation metadata", "id", conversationID, "error", err)
		h.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to update metadata: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Metadata updated successfully",
	})
}

// HandleConversationsNeedingTitles handles GET /api/v1/conversations/needs-titles
func (h *APIHandler) HandleConversationsNeedingTitles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	conversations, err := h.storage.ListConversationsNeedingTitles(ctx, limit)
	if err != nil {
		logger.Error("Failed to list conversations needing titles", "error", err)
		h.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to list conversations: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"conversations": conversations,
		"count":         len(conversations),
	})
}

// HandleListModels handles GET /api/v1/models
func (h *APIHandler) HandleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	models, err := h.modelService.ListModels(ctx)
	if err != nil {
		logger.Error("Failed to list models", "error", err)
		h.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to list models: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"models": models,
		"count":  len(models),
	})
}

// HandleAgentsStatus handles GET /api/v1/agents/status
func (h *APIHandler) HandleAgentsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agentReadiness := h.stateManager.GetAgentReadiness()

	var agents []map[string]any
	totalAgents := 0
	readyAgents := 0

	if agentReadiness != nil {
		totalAgents = agentReadiness.TotalAgents
		readyAgents = agentReadiness.ReadyAgents

		agents = make([]map[string]any, 0, len(agentReadiness.Agents))
		for _, agent := range agentReadiness.Agents {
			errorMsg := agent.Message
			if agent.Error != "" {
				errorMsg = agent.Error
			}
			agents = append(agents, map[string]any{
				"name":  agent.Name,
				"state": agent.State.String(),
				"url":   agent.URL,
				"image": agent.Image,
				"error": errorMsg,
			})
		}
	} else {
		agents = []map[string]any{}
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"total_agents": totalAgents,
		"ready_agents": readyAgents,
		"agents":       agents,
	})
}

// HandleMCPStatus handles GET /api/v1/mcp/status
func (h *APIHandler) HandleMCPStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.mcpManager == nil {
		h.writeJSON(w, http.StatusOK, map[string]any{
			"total_servers":     0,
			"connected_servers": 0,
			"total_tools":       0,
			"servers":           []map[string]any{},
		})
		return
	}

	clients := h.mcpManager.GetClients()
	totalServers := h.mcpManager.GetTotalServers()

	servers := make([]map[string]any, 0, len(clients))
	connectedServers := 0
	totalTools := 0

	for _, client := range clients {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		toolsMap, err := client.DiscoverTools(ctx)
		cancel()

		toolCount := 0
		connected := err == nil

		if connected {
			connectedServers++
			for _, tools := range toolsMap {
				toolCount += len(tools)
			}
			totalTools += toolCount
		}

		servers = append(servers, map[string]any{
			"name":      fmt.Sprintf("server-%d", len(servers)+1),
			"connected": connected,
			"tools":     toolCount,
		})
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"total_servers":     totalServers,
		"connected_servers": connectedServers,
		"total_tools":       totalTools,
		"servers":           servers,
	})
}

// HandleHistory handles history requests
// GET /api/history - Get all history commands
// POST /api/history - Save a new command to history
func (h *APIHandler) HandleHistory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleGetHistory(w, r)
	case http.MethodPost:
		h.handleSaveHistory(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetHistory retrieves all history commands
func (h *APIHandler) handleGetHistory(w http.ResponseWriter, _ *http.Request) {
	if h.historyManager == nil {
		h.writeJSON(w, http.StatusOK, map[string]any{
			"history": []string{},
			"count":   0,
		})
		return
	}

	shellHistory := h.historyManager.GetShellHistoryFile()
	shellProvider, err := history.NewShellHistoryWithDir(".infer")
	if err != nil {
		logger.Error("Failed to initialize shell history", "error", err)
		h.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to load history: %v", err),
		})
		return
	}

	commands, err := shellProvider.LoadHistory()
	if err != nil {
		logger.Error("Failed to load history", "file", shellHistory, "error", err)
		h.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to load history: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"history": commands,
		"count":   len(commands),
	})
}

// handleSaveHistory saves a new command to history
func (h *APIHandler) handleSaveHistory(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Command string `json:"command"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(request.Command) == "" {
		http.Error(w, "Command cannot be empty", http.StatusBadRequest)
		return
	}

	if h.historyManager == nil {
		logger.Warn("History manager not available")
		h.writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"message": "History not available",
		})
		return
	}

	if err := h.historyManager.AddToHistory(request.Command); err != nil {
		logger.Error("Failed to save to history", "error", err)
		h.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to save history: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Command saved to history",
	})
}
