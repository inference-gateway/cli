/**
 * TypeScript Storage Interfaces
 * Mirrors the CLI's Go domain models for conversations
 */

// Storage interface matching CLI's ConversationStorage
export interface ConversationStorage {
  saveConversation(
    conversationID: string,
    entries: ConversationEntry[],
    metadata: ConversationMetadata
  ): Promise<void>;
  loadConversation(
    conversationID: string
  ): Promise<{ entries: ConversationEntry[]; metadata: ConversationMetadata }>;
  listConversations(
    limit: number,
    offset: number
  ): Promise<ConversationSummary[]>;
  deleteConversation(conversationID: string): Promise<void>;
  updateConversationMetadata(
    conversationID: string,
    metadata: Partial<ConversationMetadata>
  ): Promise<void>;
  listConversationsNeedingTitles(limit: number): Promise<ConversationSummary[]>;
  close(): Promise<void>;
  health(): Promise<boolean>;
}

// Domain types
export interface ConversationEntry {
  message: Message;
  model?: string;
  time: Date;
  hidden?: boolean;
  images?: ImageAttachment[];
  tool_execution?: ToolExecutionResult;
  pending_tool_call?: ToolCall;
  tool_approval_status?: ToolApprovalStatus;
  rejected?: boolean;
  is_plan?: boolean;
  plan_approval_status?: PlanApprovalStatus;
}

export interface Message {
  role: "user" | "assistant" | "system";
  content: string | ContentPart[];
}

export interface ContentPart {
  type: "text" | "image";
  text?: string;
  source?: {
    type: "base64" | "url";
    media_type?: string;
    data?: string;
    url?: string;
  };
}

export interface ImageAttachment {
  data: string; // Base64 encoded
  mime_type: string;
  filename?: string;
  display_name?: string;
  source_path?: string;
}

export interface ToolExecutionResult {
  tool_name: string;
  tool_id: string;
  status: "success" | "error";
  result?: any;
  error?: string;
  timestamp?: Date;
}

export interface ToolCall {
  id: string;
  type: string;
  function: {
    name: string;
    arguments: string;
  };
}

export type ToolApprovalStatus = "pending" | "approved" | "rejected";
export type PlanApprovalStatus = "pending" | "approved" | "rejected";

export interface ConversationMetadata {
  id: string;
  title?: string;
  created_at: Date;
  updated_at: Date;
  message_count: number;
  token_stats: SessionTokenStats;
  cost_stats: SessionCostStats;
  model?: string;
  tags?: string[];
  summary?: string;
  title_generated?: boolean;
  title_invalidated?: boolean;
  title_generation_time?: Date;
}

export interface SessionTokenStats {
  total_input_tokens: number;
  total_output_tokens: number;
  request_count: number;
}

export interface SessionCostStats {
  total_cost: number;
}

export interface ConversationSummary {
  id: string;
  title?: string;
  created_at: Date;
  updated_at: Date;
  message_count: number;
  token_stats: SessionTokenStats;
  cost_stats: SessionCostStats;
  model?: string;
  tags?: string[];
  summary?: string;
  title_generated?: boolean;
  title_invalidated?: boolean;
  title_generation_time?: Date;
}

// Storage configuration types
export interface PostgresConfig {
  host: string;
  port: number;
  database: string;
  user: string;
  password: string;
}

export interface SQLiteConfig {
  path: string;
}

export interface RedisConfig {
  host: string;
  port: number;
  db?: number;
  password?: string;
}

export interface JSONLConfig {
  directory: string;
}

export type StorageType = "postgres" | "sqlite" | "redis" | "jsonl" | "memory";

export interface StorageConfig {
  type: StorageType;
  postgres?: PostgresConfig;
  sqlite?: SQLiteConfig;
  redis?: RedisConfig;
  jsonl?: JSONLConfig;
}

// Agent and MCP status types
export interface AgentStatus {
  name: string;
  state: string;
  url?: string;
  image?: string;
  error?: string;
}

export interface AgentsStatusResponse {
  total_agents: number;
  ready_agents: number;
  agents: AgentStatus[];
}

export interface MCPServerStatus {
  name: string;
  connected: boolean;
  tools: number;
}

export interface MCPStatusResponse {
  total_servers: number;
  connected_servers: number;
  total_tools: number;
  servers: MCPServerStatus[];
}
