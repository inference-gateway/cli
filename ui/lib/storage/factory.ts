import type { ConversationStorage } from "./interfaces";
import { HTTPStorage } from "./http/http-storage";

/**
 * Create storage client that communicates with the infer serve API
 *
 * This replaces direct database access with HTTP API calls to the
 * `infer serve` API server, eliminating the need for native database modules.
 */
export function createStorage(apiURL?: string): ConversationStorage {
  return new HTTPStorage(apiURL);
}

/**
 * Create storage from environment variables
 */
export function createStorageFromEnv(): ConversationStorage {
  const apiURL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
  return createStorage(apiURL);
}
