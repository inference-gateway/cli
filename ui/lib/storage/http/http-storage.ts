import type {
  ConversationStorage,
  ConversationEntry,
  ConversationMetadata,
  ConversationSummary
} from "../interfaces";
import { APIClient } from "../../api/client";

/**
 * HTTPStorage implements ConversationStorage using the infer serve REST API
 *
 * This eliminates the need for direct database access and native modules.
 * All storage operations are forwarded to the API server.
 */
export class HTTPStorage implements ConversationStorage {
  private client: APIClient;

  constructor(apiURL?: string) {
    this.client = new APIClient({ baseURL: apiURL });
  }

  async saveConversation(
    conversationID: string,
    entries: ConversationEntry[],
    metadata: ConversationMetadata
  ): Promise<void> {
    // For now, saving is handled by the headless CLI sessions directly
    // The UI is read-only for conversations
    throw new Error("Saving conversations is not supported from the UI. Use the headless CLI sessions instead.");
  }

  async loadConversation(
    conversationID: string
  ): Promise<{ entries: ConversationEntry[]; metadata: ConversationMetadata }> {
    const response = await this.client.getConversation(conversationID);
    return {
      entries: response.entries,
      metadata: response.metadata
    };
  }

  async listConversations(
    limit: number,
    offset: number
  ): Promise<ConversationSummary[]> {
    const response = await this.client.listConversations(limit, offset);
    return response.conversations;
  }

  async deleteConversation(conversationID: string): Promise<void> {
    await this.client.deleteConversation(conversationID);
  }

  async updateConversationMetadata(
    conversationID: string,
    metadata: Partial<ConversationMetadata>
  ): Promise<void> {
    await this.client.updateMetadata(conversationID, metadata);
  }

  async listConversationsNeedingTitles(limit: number): Promise<ConversationSummary[]> {
    const response = await this.client.listConversationsNeedingTitles(limit);
    return response.conversations;
  }

  async close(): Promise<void> {
    // No connection to close for HTTP client
  }

  async health(): Promise<boolean> {
    try {
      const response = await this.client.health();
      return response.status === "healthy";
    } catch {
      return false;
    }
  }
}
