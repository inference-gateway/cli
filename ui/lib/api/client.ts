/**
 * HTTP Client for Inference Gateway API
 *
 * This client communicates with the `infer serve` REST API to access
 * conversation storage without requiring direct database access.
 */

import type {
  ConversationSummary,
  ConversationMetadata,
  ConversationEntry,
  AgentsStatusResponse,
  MCPStatusResponse
} from "../storage/interfaces";

export interface APIClientConfig {
  baseURL?: string;
  timeout?: number;
}

export interface ListConversationsResponse {
  conversations: ConversationSummary[];
  count: number;
  limit: number;
  offset: number;
}

export interface GetConversationResponse {
  id: string;
  entries: ConversationEntry[];
  metadata: ConversationMetadata;
}

export interface HealthResponse {
  status: "healthy" | "unhealthy";
  time: string;
  error?: string;
}

export class APIClient {
  private baseURL: string;
  private timeout: number;

  constructor(config: APIClientConfig = {}) {
    this.baseURL = config.baseURL || process.env.NEXT_PUBLIC_API_URL || "http://localhost:8081";
    this.timeout = config.timeout || 30000;
  }

  private async fetch<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    const url = `${this.baseURL}${endpoint}`;
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), this.timeout);

    try {
      const response = await fetch(url, {
        ...options,
        signal: controller.signal,
        headers: {
          "Content-Type": "application/json",
          ...options.headers,
        },
      });

      clearTimeout(timeoutId);

      if (!response.ok) {
        const error = await response.json().catch(() => ({ error: response.statusText }));
        throw new Error(error.error || `HTTP ${response.status}: ${response.statusText}`);
      }

      return await response.json();
    } catch (error) {
      clearTimeout(timeoutId);
      if (error instanceof Error) {
        if (error.name === "AbortError") {
          throw new Error("Request timeout");
        }
        throw error;
      }
      throw new Error("Unknown error");
    }
  }

  /**
   * Check API health
   */
  async health(): Promise<HealthResponse> {
    return this.fetch<HealthResponse>("/health");
  }

  /**
   * List conversations with pagination
   */
  async listConversations(
    limit: number = 50,
    offset: number = 0
  ): Promise<ListConversationsResponse> {
    return this.fetch<ListConversationsResponse>(
      `/api/v1/conversations?limit=${limit}&offset=${offset}`
    );
  }

  /**
   * Get a specific conversation by ID
   */
  async getConversation(id: string): Promise<GetConversationResponse> {
    return this.fetch<GetConversationResponse>(`/api/v1/conversations/${id}`);
  }

  /**
   * Delete a conversation by ID
   */
  async deleteConversation(id: string): Promise<{ success: boolean; message: string }> {
    return this.fetch<{ success: boolean; message: string }>(
      `/api/v1/conversations/${id}`,
      { method: "DELETE" }
    );
  }

  /**
   * Update conversation metadata
   */
  async updateMetadata(
    id: string,
    metadata: Partial<ConversationMetadata>
  ): Promise<{ success: boolean; message: string }> {
    return this.fetch<{ success: boolean; message: string }>(
      `/api/v1/conversations/${id}/metadata`,
      {
        method: "PATCH",
        body: JSON.stringify(metadata),
      }
    );
  }

  /**
   * List conversations that need title generation
   */
  async listConversationsNeedingTitles(
    limit: number = 10
  ): Promise<{ conversations: ConversationSummary[]; count: number }> {
    return this.fetch<{ conversations: ConversationSummary[]; count: number }>(
      `/api/v1/conversations/needs-titles?limit=${limit}`
    );
  }

  /**
   * List available models
   */
  async listModels(): Promise<{ models: string[]; count: number }> {
    return this.fetch<{ models: string[]; count: number }>(
      `/api/v1/models`
    );
  }

  /**
   * Get A2A agents status
   */
  async getAgentsStatus(): Promise<AgentsStatusResponse> {
    return this.fetch<AgentsStatusResponse>(`/api/v1/agents/status`);
  }

  /**
   * Get MCP servers status
   */
  async getMCPStatus(): Promise<MCPStatusResponse> {
    return this.fetch<MCPStatusResponse>(`/api/v1/mcp/status`);
  }

  /**
   * Get command history
   */
  async getHistory(): Promise<{ history: string[]; count: number }> {
    return this.fetch<{ history: string[]; count: number }>(`/api/history`);
  }

  /**
   * Save command to history
   */
  async saveToHistory(command: string): Promise<{ success: boolean; message: string }> {
    return this.fetch<{ success: boolean; message: string }>(
      `/api/history`,
      {
        method: "POST",
        body: JSON.stringify({ command }),
      }
    );
  }
}

// Export singleton instance
export const apiClient = new APIClient();

/**
 * React Query Hooks
 */

import { useQuery } from "@tanstack/react-query";

/**
 * Hook to fetch agents status with auto-refresh
 */
export function useAgentsStatus(refreshInterval: number = 5000) {
  return useQuery({
    queryKey: ["agents-status"],
    queryFn: () => apiClient.getAgentsStatus(),
    refetchInterval: refreshInterval,
    refetchOnWindowFocus: true,
    staleTime: 2000, // Consider data stale after 2 seconds
  });
}

/**
 * Hook to fetch MCP status with auto-refresh
 */
export function useMCPStatus(refreshInterval: number = 5000) {
  return useQuery({
    queryKey: ["mcp-status"],
    queryFn: () => apiClient.getMCPStatus(),
    refetchInterval: refreshInterval,
    refetchOnWindowFocus: true,
    staleTime: 2000, // Consider data stale after 2 seconds
  });
}
