/**
 * AG-UI Protocol Type Definitions
 * Reference: https://docs.ag-ui.com
 */

export type ToolStatus = 'queued' | 'running' | 'complete' | 'failed';

export interface ToolCallStartEvent {
  type: 'ToolCallStart';
  toolCallId: string;
  toolCallName: string;
  parentMessageId?: string;
  status?: ToolStatus;
  metadata?: Record<string, any>;
  timestamp?: number;
}

export interface ToolCallArgsEvent {
  type: 'ToolCallArgs';
  toolCallId: string;
  delta: string;
  timestamp?: number;
}

export interface ToolCallProgressEvent {
  type: 'ToolCallProgress';
  toolCallId: string;
  status: ToolStatus;
  message: string;
  output?: string;
  metadata?: Record<string, any>;
  timestamp?: number;
}

export interface ToolCallResultEvent {
  type: 'ToolCallResult';
  messageId: string;
  toolCallId: string;
  content: any;
  role?: string;
  status?: ToolStatus;
  duration?: number;
  metadata?: Record<string, any>;
  timestamp?: number;
}

export interface ParallelToolsMetadataEvent {
  type: 'ParallelToolsMetadata';
  totalCount: number;
  successCount?: number;
  failureCount?: number;
  totalDuration?: number;
  timestamp?: number;
}

export interface ToolCallState {
  id: string;
  name: string;
  status: ToolStatus;
  message?: string;
  output?: string;
  arguments?: string;
  startTime?: number;
  duration?: number;
  isComplete: boolean;
}

export type AGUIEvent =
  | ToolCallStartEvent
  | ToolCallArgsEvent
  | ToolCallProgressEvent
  | ToolCallResultEvent
  | ParallelToolsMetadataEvent;
