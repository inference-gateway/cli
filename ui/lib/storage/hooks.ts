import { useQuery, useMutation, useQueryClient, UseQueryOptions } from "@tanstack/react-query";
import * as React from "react";
import type {
  ConversationStorage,
  ConversationSummary,
  ConversationEntry,
  ConversationMetadata,
} from "./interfaces";

/**
 * React hooks for accessing conversation storage
 * Uses React Query for caching and state management
 */

// Storage instance should be initialized once and reused
let storageInstance: ConversationStorage | null = null;

export function setStorageInstance(storage: ConversationStorage) {
  storageInstance = storage;
}

export function getStorageInstance(): ConversationStorage {
  if (!storageInstance) {
    throw new Error("Storage instance not initialized. Call setStorageInstance() first.");
  }
  return storageInstance;
}

// ============================================================================
// Query Hooks
// ============================================================================

/**
 * Hook to list conversations with pagination
 */
export function useConversations(
  limit: number = 50,
  offset: number = 0,
  options?: Omit<UseQueryOptions<ConversationSummary[]>, "queryKey" | "queryFn">
) {
  const storage = getStorageInstance();

  return useQuery<ConversationSummary[]>({
    queryKey: ["conversations", limit, offset],
    queryFn: () => storage.listConversations(limit, offset),
    ...options,
  });
}

/**
 * Hook to get a specific conversation with full details
 */
export function useConversation(
  conversationId: string,
  options?: Omit<
    UseQueryOptions<{ entries: ConversationEntry[]; metadata: ConversationMetadata }>,
    "queryKey" | "queryFn"
  >
) {
  const storage = getStorageInstance();

  return useQuery({
    queryKey: ["conversation", conversationId],
    queryFn: () => storage.loadConversation(conversationId),
    enabled: !!conversationId,
    ...options,
  });
}

/**
 * Hook to check storage health
 */
export function useStorageHealth(
  options?: Omit<UseQueryOptions<boolean>, "queryKey" | "queryFn">
) {
  const storage = getStorageInstance();

  return useQuery({
    queryKey: ["storage", "health"],
    queryFn: () => storage.health(),
    ...options,
  });
}

// ============================================================================
// Mutation Hooks
// ============================================================================

/**
 * Hook to delete a conversation
 */
export function useDeleteConversation() {
  const storage = getStorageInstance();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (conversationId: string) => storage.deleteConversation(conversationId),
    onSuccess: () => {
      // Invalidate conversations list to refetch
      queryClient.invalidateQueries({ queryKey: ["conversations"] });
    },
  });
}

/**
 * Hook to update conversation metadata (title, tags, etc.)
 */
export function useUpdateConversationMetadata() {
  const storage = getStorageInstance();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({
      conversationId,
      metadata,
    }: {
      conversationId: string;
      metadata: Partial<ConversationMetadata>;
    }) => storage.updateConversationMetadata(conversationId, metadata),
    onSuccess: (_, variables) => {
      // Invalidate specific conversation and list
      queryClient.invalidateQueries({ queryKey: ["conversation", variables.conversationId] });
      queryClient.invalidateQueries({ queryKey: ["conversations"] });
    },
  });
}

/**
 * Hook to save a conversation
 */
export function useSaveConversation() {
  const storage = getStorageInstance();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({
      conversationId,
      entries,
      metadata,
    }: {
      conversationId: string;
      entries: ConversationEntry[];
      metadata: ConversationMetadata;
    }) => storage.saveConversation(conversationId, entries, metadata),
    onSuccess: (_, variables) => {
      // Invalidate specific conversation and list
      queryClient.invalidateQueries({ queryKey: ["conversation", variables.conversationId] });
      queryClient.invalidateQueries({ queryKey: ["conversations"] });
    },
  });
}

// ============================================================================
// Helper Hooks
// ============================================================================

/**
 * Hook to get conversation statistics
 * Aggregates data from all conversations
 */
export function useConversationStats() {
  const { data: conversations, ...query } = useConversations(1000, 0);

  const stats = React.useMemo(() => {
    if (!conversations) {
      return null;
    }

    const totalConversations = conversations.length;
    const totalMessages = conversations.reduce(
      (sum, conv) => sum + conv.message_count,
      0
    );
    const totalInputTokens = conversations.reduce(
      (sum, conv) => sum + conv.token_stats.total_input_tokens,
      0
    );
    const totalOutputTokens = conversations.reduce(
      (sum, conv) => sum + conv.token_stats.total_output_tokens,
      0
    );
    const totalCost = conversations.reduce(
      (sum, conv) => sum + conv.cost_stats.total_cost,
      0
    );

    const models = new Set(
      conversations.filter((c) => c.model).map((c) => c.model!)
    );

    return {
      totalConversations,
      totalMessages,
      totalInputTokens,
      totalOutputTokens,
      totalTokens: totalInputTokens + totalOutputTokens,
      totalCost,
      models: Array.from(models),
    };
  }, [conversations]);

  return {
    ...query,
    data: stats,
  };
}
