"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import { apiClient } from "../lib/api/client";
import type { ConversationSummary } from "../lib/storage/interfaces";
import { WebSocketChatClient } from "../lib/chat/websocket-client";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Check, ChevronsUpDown, Send as SendIcon, Square } from "lucide-react";
import { cn } from "@/lib/utils";
import { ThemeToggle } from "@/components/theme-toggle";
import StatusBar from "@/components/status-bar";
import { ToolCallDisplay } from "@/components/tool-call-display";
import type { ToolCallState } from "@/lib/agui-types";

// Utility to strip ANSI color codes from terminal output
function stripAnsiCodes(text: string): string {
  return text.replace(/\x1B\[[0-9;]*[a-zA-Z]/g, '').replace(/\[[\d;]+m/g, '');
}

export default function Home() {
  const [conversations, setConversations] = useState<ConversationSummary[]>([]);
  const [selectedConversation, setSelectedConversation] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [view, setView] = useState<"chat" | "dashboard">("chat");
  const [liveSessionId, setLiveSessionId] = useState<string | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [hasActiveNewChat, setHasActiveNewChat] = useState(false);
  const [sessionRestored, setSessionRestored] = useState(false);
  const [chatKey, setChatKey] = useState(0);

  useEffect(() => {
    if (loading || sessionRestored) return;

    const savedSessionId = localStorage.getItem("currentSessionId");
    const savedSessionType = localStorage.getItem("currentSessionType");

    if (savedSessionId && savedSessionType === "new") {
      setLiveSessionId("new");
      setHasActiveNewChat(true);
      setSessionRestored(true);
    } else if (savedSessionId && savedSessionType === "conversation") {
      if (conversations.some(c => c.id === savedSessionId)) {
        setSelectedConversation(savedSessionId);
        setLiveSessionId(savedSessionId);
        setHasActiveNewChat(false);
        setSessionRestored(true);
      } else if (conversations.length > 0 || !loading) {
        console.warn("Saved conversation not found, clearing localStorage");
        localStorage.removeItem("currentSessionId");
        localStorage.removeItem("currentSessionType");
        setSessionRestored(true);
      }
    } else {
      setSessionRestored(true);
    }
  }, [conversations, loading, sessionRestored]);

  useEffect(() => {
    loadConversations();
  }, []);

  const loadConversations = async () => {
    try {
      setLoading(true);
      const response = await apiClient.listConversations(50, 0);
      setConversations(response.conversations);
    } catch (error) {
      console.error("Failed to load conversations:", error);
    } finally {
      setLoading(false);
    }
  };

  const handleNewChat = () => {
    loadConversations();

    setLiveSessionId("new");
    setSelectedConversation(null);
    setHasActiveNewChat(true);
    setSidebarOpen(false);
    setChatKey(prev => prev + 1);
    localStorage.setItem("currentSessionId", "new");
    localStorage.setItem("currentSessionType", "new");

    localStorage.removeItem("wsSession_new-chat");
    localStorage.removeItem("conversationId_new-chat");
  };

  const handleContinueConversation = (conversationId: string) => {
    setSelectedConversation(conversationId);
    setLiveSessionId(conversationId);
    setHasActiveNewChat(false);
    setSidebarOpen(false);
    localStorage.setItem("currentSessionId", conversationId);
    localStorage.setItem("currentSessionType", "conversation");
  };

  const handleDeleteConversation = async (conversationId: string, event: React.MouseEvent) => {
    event.stopPropagation();

    if (!confirm("Are you sure you want to delete this conversation? This cannot be undone.")) {
      return;
    }

    try {
      await apiClient.deleteConversation(conversationId);

      setConversations(prev => prev.filter(c => c.id !== conversationId));

      if (selectedConversation === conversationId) {
        setSelectedConversation(null);
        setLiveSessionId(null);
      }
    } catch (error) {
      console.error("Failed to delete conversation:", error);
      alert("Failed to delete conversation. Please try again.");
    }
  };

  return (
    <div className="flex h-screen bg-gray-100 dark:bg-gray-900">
      {!sidebarOpen && (
        <button
          onClick={() => setSidebarOpen(true)}
          className="md:hidden fixed top-4 left-4 z-50 p-2 bg-blue-600 text-white rounded-lg shadow-lg"
        >
          <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>
      )}

      {sidebarOpen && (
        <div
          className="md:hidden fixed inset-0 bg-black bg-opacity-50 z-30"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      <div className={`
        ${sidebarOpen ? 'translate-x-0' : '-translate-x-full'}
        md:translate-x-0
        fixed md:relative
        w-80 h-full
        bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700
        flex flex-col
        transition-transform duration-300 ease-in-out
        z-40
      `}>
        <div className="p-4 border-b border-gray-200 dark:border-gray-700 bg-gradient-to-r from-blue-600 to-blue-700 relative">
          <div className="flex items-center justify-between gap-2">
            <div>
              <h1 className="text-base font-semibold text-white">Inference Gateway</h1>
              <p className="text-xs text-blue-100">Chat UI</p>
            </div>
            <div className="flex items-center gap-2">
              <ThemeToggle />
              <button
                onClick={() => setSidebarOpen(false)}
                className="md:hidden text-white hover:bg-blue-800 rounded p-1"
              >
                <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          </div>
        </div>
        <div className="p-4 border-b border-gray-200 dark:border-gray-700">
          <button
            onClick={handleNewChat}
            className="w-full bg-blue-600 hover:bg-blue-700 text-white font-medium py-3 px-4 rounded-lg transition-colors flex items-center justify-center gap-2"
          >
            <span className="text-lg">+</span>
            New Chat
          </button>
        </div>
        <div className="flex border-b border-gray-200 dark:border-gray-700">
          <button
            onClick={() => setView("chat")}
            className={`flex-1 py-3 text-sm font-medium transition-colors ${
              view === "chat"
                ? "text-blue-600 dark:text-blue-400 border-b-2 border-blue-600 dark:border-blue-400"
                : "text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200"
            }`}
          >
            💬 Conversations
          </button>
          <button
            onClick={() => setView("dashboard")}
            className={`flex-1 py-3 text-sm font-medium transition-colors ${
              view === "dashboard"
                ? "text-blue-600 dark:text-blue-400 border-b-2 border-blue-600 dark:border-blue-400"
                : "text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200"
            }`}
          >
            📊 Stats
          </button>
        </div>

        {/* Conversations List */}
        <div className="flex-1 overflow-y-auto">
          {view === "chat" && (
            <>
              {loading ? (
                <div className="p-4 text-center text-gray-500 dark:text-gray-400">
                  <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 mx-auto"></div>
                  <p className="mt-2 text-sm">Loading conversations...</p>
                </div>
              ) : conversations.length === 0 && !hasActiveNewChat ? (
                <div className="p-4 text-center text-gray-500 dark:text-gray-400">
                  <p className="text-sm">No conversations yet</p>
                  <p className="text-xs mt-2">Start a headless CLI session to create one</p>
                </div>
              ) : (
                <>
                  {hasActiveNewChat && (
                    <div className="relative group border-b border-gray-100 dark:border-gray-700 bg-blue-50 dark:bg-blue-900/30 border-l-4 border-l-blue-600 dark:border-l-blue-400">
                      <div className="w-full text-left p-4 pr-12">
                        <div className="font-medium text-gray-900 dark:text-gray-100 truncate flex items-center gap-2">
                          <span className="animate-pulse">●</span> New Chat
                        </div>
                        <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          Active session
                        </div>
                        <div className="text-xs text-gray-400 dark:text-gray-500 mt-1">
                          {new Date().toLocaleDateString()}
                        </div>
                      </div>
                    </div>
                  )}
                  {conversations.map((conv) => (
                  <div
                    key={conv.id}
                    className={`relative group border-b border-gray-100 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors ${
                      selectedConversation === conv.id ? "bg-blue-50 dark:bg-blue-900/30 border-l-4 border-l-blue-600 dark:border-l-blue-400" : ""
                    }`}
                  >
                    <button
                      onClick={() => handleContinueConversation(conv.id)}
                      className="w-full text-left p-4 pr-12"
                    >
                      <div className="font-medium text-gray-900 dark:text-gray-100 truncate">
                        {conv.title || "Untitled Conversation"}
                      </div>
                      <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                        {conv.message_count} messages • {conv.token_stats.total_input_tokens + conv.token_stats.total_output_tokens} tokens
                      </div>
                      <div className="text-xs text-gray-400 dark:text-gray-500 mt-1">
                        {new Date(conv.updated_at).toLocaleDateString()}
                      </div>
                    </button>
                    <button
                      onClick={(e) => handleDeleteConversation(conv.id, e)}
                      className="absolute right-2 top-1/2 -translate-y-1/2 p-2 rounded-lg hover:bg-red-100 dark:hover:bg-red-900/30 text-gray-400 dark:text-gray-500 hover:text-red-600 dark:hover:text-red-400 opacity-0 group-hover:opacity-100 transition-opacity"
                      title="Delete conversation"
                    >
                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                      </svg>
                    </button>
                  </div>
                  ))}
                </>
              )}
            </>
          )}

          {view === "dashboard" && (
            <div className="p-4 space-y-4">
              <div className="bg-gradient-to-br from-purple-50 to-purple-100 rounded-lg p-4">
                <div className="text-xs text-purple-600 font-medium">Total Conversations</div>
                <div className="text-2xl font-bold text-purple-900 mt-1">{conversations.length}</div>
              </div>

              <div className="bg-gradient-to-br from-blue-50 to-blue-100 rounded-lg p-4">
                <div className="text-xs text-blue-600 font-medium">Total Messages</div>
                <div className="text-2xl font-bold text-blue-900 mt-1">
                  {conversations.reduce((sum, c) => sum + c.message_count, 0)}
                </div>
              </div>

              <div className="bg-gradient-to-br from-green-50 to-green-100 rounded-lg p-4">
                <div className="text-xs text-green-600 font-medium">Total Tokens</div>
                <div className="text-2xl font-bold text-green-900 mt-1">
                  {conversations.reduce((sum, c) => sum + c.token_stats.total_input_tokens + c.token_stats.total_output_tokens, 0).toLocaleString()}
                </div>
              </div>

              <div className="bg-gradient-to-br from-orange-50 to-orange-100 rounded-lg p-4">
                <div className="text-xs text-orange-600 font-medium">Total Cost</div>
                <div className="text-2xl font-bold text-orange-900 mt-1">
                  ${conversations.reduce((sum, c) => sum + (c.cost_stats?.total_cost || 0), 0).toFixed(4)}
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="p-3 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
          <div className="flex items-center justify-between text-xs text-gray-500 dark:text-gray-400">
            <span>API Server</span>
            <span className="font-mono bg-green-100 dark:bg-green-900 text-green-700 dark:text-green-300 px-2 py-1 rounded">:8081</span>
          </div>
        </div>
      </div>

      {/* Main Chat Area */}
      <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
        {liveSessionId ? (
          <LiveChatView
            key={liveSessionId === "new" ? `new-chat-${chatKey}` : liveSessionId}
            conversationId={liveSessionId === "new" ? undefined : liveSessionId}
            onClose={() => {
              setLiveSessionId(null);
              setSelectedConversation(null);
              setHasActiveNewChat(false);
              localStorage.removeItem("currentSessionId");
              localStorage.removeItem("currentSessionType");
              loadConversations();
            }}
          />
        ) : (
          <EmptyState />
        )}
      </div>
    </div>
  );
}

// ChatView is now integrated into LiveChatView - removed to avoid duplication

type TimelineItem =
  | { type: 'message'; id: string; role: string; content: string }
  | { type: 'tool'; id: string; name: string; status: ToolCallState['status']; startTime?: number; duration?: number; message?: string; output?: string; arguments?: string; result?: any };

function LiveChatView({ conversationId, onClose }: { conversationId?: string; onClose: () => void }) {
  const [timeline, setTimeline] = useState<TimelineItem[]>([]);
  const [inputMessage, setInputMessage] = useState("");
  const [isConnecting, setIsConnecting] = useState(true);
  const [isSending, setIsSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [models, setModels] = useState<string[]>([]);
  const [selectedModel, setSelectedModel] = useState<string>("");
  const [loadingModels, setLoadingModels] = useState(true);
  const [modelSelectorOpen, setModelSelectorOpen] = useState(false);
  const wsClientRef = useRef<WebSocketChatClient | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const sendingTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const cleanupTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const isMountedRef = useRef(true);
  const actualConversationIdRef = useRef<string | undefined>(conversationId);
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const [currentDraft, setCurrentDraft] = useState("");

  useEffect(() => {
    apiClient
      .listModels()
      .then((data) => {
        setModels(data.models);

        const savedModel = localStorage.getItem("selectedModel");

        if (savedModel && data.models.includes(savedModel)) {
          setSelectedModel(savedModel);
        } else if (data.models.length > 0) {
          setSelectedModel(data.models[0]);
        }

        setLoadingModels(false);
      })
      .catch((err) => {
        console.error("Failed to load models:", err);
        setLoadingModels(false);
      });

    apiClient
      .getHistory()
      .then((data) => {
        setHistory(data.history);
      })
      .catch((err) => {
        console.error("Failed to load history:", err);
      });
  }, []);

  useEffect(() => {
    isMountedRef.current = true;

    if (sendingTimeoutRef.current) {
      clearTimeout(sendingTimeoutRef.current);
      sendingTimeoutRef.current = null;
    }

    const loadExistingConversation = async () => {
      if (conversationId) {
        try {
          const data = await apiClient.getConversation(conversationId);
          const loadedTimeline: TimelineItem[] = data.entries
            .filter((entry: any) => {
              return entry.message.hidden !== true;
            })
            .map((entry: any, index: number) => ({
              type: 'message' as const,
              id: `msg-${index}`,
              role: entry.message.role,
              content: typeof entry.message.content === "string"
                ? stripAnsiCodes(entry.message.content)
                : JSON.stringify(entry.message.content)
            }));
          setTimeline(loadedTimeline);
        } catch (error: any) {
          console.error("[LiveChatView] Failed to load conversation:", error);
          setError(error.message || "Failed to load conversation");
          setIsConnecting(false);
        }
      } else {
        setTimeline([]);
      }
    };

    loadExistingConversation();

    // Create session ID based on conversation to ensure proper reuse
    // Same conversation = same session (reuse containers)
    // Different conversation = different session (fresh start)
    const sessionKey = conversationId || "new-chat";
    let wsSessionId = localStorage.getItem(`wsSession_${sessionKey}`);

    if (!wsSessionId) {
      wsSessionId = `ws-${Date.now()}-${Math.random().toString(36).substring(2, 11)}`;
      localStorage.setItem(`wsSession_${sessionKey}`, wsSessionId);
    }

    const storedConversationId = localStorage.getItem(`conversationId_${sessionKey}`);
    const effectiveConversationId = storedConversationId || conversationId;

    if (storedConversationId) {
      actualConversationIdRef.current = storedConversationId;
    }

    const client = new WebSocketChatClient();
    wsClientRef.current = client;

    const unsubscribe = client.onMessage((event) => {
      if (event.type === "error") {
        setError(event.data.error);
        if (sendingTimeoutRef.current) {
          clearTimeout(sendingTimeoutRef.current);
          sendingTimeoutRef.current = null;
        }
        setIsSending(false);
        return;
      }

      try {
        const data = typeof event.data === "string" ? JSON.parse(event.data) : event.data;

        console.log('[WS Event]', data.type);

        switch (data.type) {
          case "TextMessageStart":
            setTimeline((prev) => [
              ...prev,
              {
                type: 'message',
                id: data.messageId || `msg-${Date.now()}`,
                role: data.role || 'assistant',
                content: ''
              }
            ]);
            break;

          case "TextMessageContent":
            setTimeline((prev) => {
              const last = prev[prev.length - 1];
              if (last && last.type === 'message') {
                return [
                  ...prev.slice(0, -1),
                  { ...last, content: last.content + (data.delta || '') }
                ];
              }
              return prev;
            });
            break;

          case "TextMessageEnd":
            // Message complete (no action needed)
            break;

          case "RunStarted":
            if (data.input) {
              setIsSending(true);
              if (sendingTimeoutRef.current) {
                clearTimeout(sendingTimeoutRef.current);
              }
              sendingTimeoutRef.current = setTimeout(() => {
                console.warn("Run timeout - no RunFinished/RunError received within 5 minutes");
                setIsSending(false);
                sendingTimeoutRef.current = null;
              }, 5 * 60 * 1000);
            }
            break;

          case "RunFinished":
            if (sendingTimeoutRef.current) {
              clearTimeout(sendingTimeoutRef.current);
              sendingTimeoutRef.current = null;
            }
            setIsSending(false);
            break;

          case "RunError":
            console.error("Run error:", data);
            if (sendingTimeoutRef.current) {
              clearTimeout(sendingTimeoutRef.current);
              sendingTimeoutRef.current = null;
            }
            setIsSending(false);
            if (data.message) {
              setTimeline((prev) => [
                ...prev,
                { type: 'message', id: `err-${Date.now()}`, role: "assistant", content: `Error: ${data.message}` }
              ]);
            }
            break;

          case "ToolCallStart":
            setTimeline((prev) => [
              ...prev,
              {
                type: 'tool',
                id: data.toolCallId,
                name: data.toolCallName,
                status: data.status || 'queued',
                startTime: data.timestamp || Date.now(),
                arguments: data.arguments || '',
              }
            ]);
            break;

          case "ToolCallArgs":
            console.log('[ToolCallArgs]', data.toolCallId, 'delta:', data.delta);
            setTimeline((prev) =>
              prev.map(item =>
                item.type === 'tool' && item.id === data.toolCallId
                  ? {
                      ...item,
                      arguments: (item.arguments || '') + data.delta,
                    }
                  : item
              )
            );
            break;

          case "ToolCallEnd":
            console.log('[ToolCallEnd]', data.toolCallId);
            // Tool call specification complete (no UI action needed)
            break;

          case "ToolCallProgress":
            setTimeline((prev) =>
              prev.map(item =>
                item.type === 'tool' && item.id === data.toolCallId
                  ? {
                      ...item,
                      status: data.status,
                      message: data.message,
                      output: data.output ? (item.output || '') + data.output : item.output,
                    }
                  : item
              )
            );
            break;

          case "ToolCallResult":
            setTimeline((prev) =>
              prev.map(item =>
                item.type === 'tool' && item.id === data.toolCallId
                  ? {
                      ...item,
                      status: data.status || 'complete',
                      duration: data.duration,
                      output: typeof data.content === 'string' ? data.content : JSON.stringify(data.content, null, 2),
                    }
                  : item
              )
            );
            break;

          case "ParallelToolsMetadata":
            console.log("Parallel tools completed:", {
              total: data.totalCount,
              success: data.successCount,
              failed: data.failureCount,
              duration: data.totalDuration,
            });
            break;

          case "session_created":
            if (data.conversation_id) {
              actualConversationIdRef.current = data.conversation_id;

              const sessionKey = conversationId || "new-chat";
              localStorage.setItem(`conversationId_${sessionKey}`, data.conversation_id);
            }
            break;

          case "ConversationCreated":
            if (data.conversation_id) {
              actualConversationIdRef.current = data.conversation_id;

              const sessionKey = conversationId || "new-chat";
              localStorage.setItem(`conversationId_${sessionKey}`, data.conversation_id);
            }
            break;

          default:
            console.log("Unknown event type:", data.type || event.type);
            console.log("Event data:", JSON.stringify(data, null, 2));
            break;
        }
      } catch (error) {
        console.error("Failed to parse message:", error);
      }
    });

    client.createSession(effectiveConversationId, wsSessionId)
      .then((returnedSessionId) => {
        setIsConnecting(false);
      })
      .catch((err) => {
        console.error("[LiveChatView] Failed to create session:", err);
        setError(`Failed to create session: ${err.message}`);
        setIsConnecting(false);
      });

    return () => {
      isMountedRef.current = false;

      unsubscribe();

      if (sendingTimeoutRef.current) {
        clearTimeout(sendingTimeoutRef.current);
        sendingTimeoutRef.current = null;
      }

      if (cleanupTimeoutRef.current) {
        clearTimeout(cleanupTimeoutRef.current);
      }

      cleanupTimeoutRef.current = setTimeout(() => {
        if (!isMountedRef.current && wsClientRef.current) {
          wsClientRef.current.close();
          wsClientRef.current = null;
        }
      }, 150);
    };
  }, [conversationId]);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [timeline]);

  const handleStop = useCallback(() => {
    if (!wsClientRef.current) return;

    try {
      wsClientRef.current.interrupt();
      if (sendingTimeoutRef.current) {
        clearTimeout(sendingTimeoutRef.current);
        sendingTimeoutRef.current = null;
      }
      setIsSending(false);
    } catch (err: any) {
      console.error("[LiveChatView] Failed to stop response:", err);
    }
  }, []);

  const handleSend = () => {
    if (!inputMessage.trim() || !wsClientRef.current) return;

    const messageContent = inputMessage;

    try {
      wsClientRef.current.sendMessage(messageContent, [], selectedModel);

      apiClient.saveToHistory(messageContent).catch((err) => {
        console.error("Failed to save to history:", err);
      });
      setHistory((prev) => [...prev, messageContent]);

      setInputMessage("");
      setHistoryIndex(-1);
      setCurrentDraft("");
    } catch (err: any) {
      setError(`Failed to send message: ${err.message}`);
    }
  };

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && isSending && wsClientRef.current) {
        handleStop();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [isSending, handleStop]);

  useEffect(() => {
    if (!isSending && !isConnecting && inputRef.current) {
      inputRef.current.focus();
    }
  }, [isSending, isConnecting]);

  if (isConnecting) {
    return (
      <div className="flex-1 flex items-center justify-center bg-gray-50 dark:bg-gray-900">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600 mx-auto"></div>
          <p className="mt-4 text-gray-500 dark:text-gray-400">Starting new chat session...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex-1 flex items-center justify-center bg-gray-50 dark:bg-gray-900">
        <div className="text-center max-w-md">
          <div className="text-red-500 dark:text-red-400 text-4xl mb-4">⚠️</div>
          <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-2">Connection Error</h3>
          <p className="text-gray-600 dark:text-gray-400 mb-4">{error}</p>
          <button
            onClick={onClose}
            className="bg-blue-600 hover:bg-blue-700 text-white font-medium py-2 px-6 rounded transition-colors"
          >
            Go Back
          </button>
        </div>
      </div>
    );
  }

  return (
    <>
      <div className="bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 p-4">
        <div className="flex flex-col md:flex-row items-center gap-3 md:gap-4">
          <div className="text-center md:text-left">
            <h2 className="font-semibold text-gray-900 dark:text-gray-100 text-sm md:text-base">{conversationId ? "Continue Conversation" : "New Chat Session"}</h2>
            <p className="text-xs text-gray-500 dark:text-gray-400">Live chat via WebSocket</p>
          </div>

          <div className="hidden md:block md:flex-1"></div>

          {loadingModels ? (
            <div className="text-sm text-gray-500 dark:text-gray-400">Loading...</div>
          ) : (
            <div className="w-full md:w-auto">
              <Popover open={modelSelectorOpen} onOpenChange={setModelSelectorOpen}>
                <PopoverTrigger asChild>
                  <Button
                    variant="outline"
                    role="combobox"
                    aria-expanded={modelSelectorOpen}
                    className="w-full md:w-[300px] justify-between text-xs md:text-sm"
                  >
                    <span className="truncate">{selectedModel || "Select model..."}</span>
                    <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
                  </Button>
                </PopoverTrigger>
                <PopoverContent className="w-[calc(100vw-2rem)] md:w-[300px] p-0">
                  <Command shouldFilter={true}>
                    <CommandInput placeholder="Search models..." className="h-9" />
                    <CommandList>
                      <CommandEmpty>No model found.</CommandEmpty>
                      <CommandGroup>
                        {models.map((model) => {
                          const searchTerms = model.toLowerCase().split(/[\/\-\s]+/);
                          return (
                            <CommandItem
                              key={model}
                              value={model}
                              keywords={searchTerms}
                              onSelect={() => {
                                setSelectedModel(model);
                                localStorage.setItem("selectedModel", model);
                                setModelSelectorOpen(false);
                              }}
                            >
                              {model}
                              <Check
                                className={cn(
                                  "ml-auto h-4 w-4",
                                  selectedModel === model ? "opacity-100" : "opacity-0"
                                )}
                              />
                            </CommandItem>
                          );
                        })}
                      </CommandGroup>
                    </CommandList>
                  </Command>
                </PopoverContent>
              </Popover>
            </div>
          )}

          <div className="hidden md:block md:flex-1"></div>

          <button
            onClick={onClose}
            className="hidden md:block text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 px-3 py-1 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-sm whitespace-nowrap"
          >
            ✕ Close
          </button>
        </div>
      </div>
      <div className="flex-1 overflow-y-auto bg-gray-50 dark:bg-gray-900 p-3 md:p-6">
        {timeline.length === 0 ? (
          <div className="h-full flex items-center justify-center px-4">
            <p className="text-gray-400 dark:text-gray-500 text-lg md:text-2xl text-center">How can I help you today?</p>
          </div>
        ) : (
          <div className="max-w-4xl mx-auto space-y-4 md:space-y-6 py-2">
            {timeline.map((item, index) =>
              item.type === 'message' ? (
                <div
                  key={item.id}
                  className={`flex ${item.role === "user" ? "justify-end" : "justify-start"}`}
                >
                  <div
                    className={`max-w-[85%] md:max-w-[75%] rounded-2xl px-3 md:px-5 py-2 md:py-3 ${
                      item.role === "user"
                        ? "bg-blue-600 text-white"
                        : "bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 shadow-sm border border-gray-200 dark:border-gray-700"
                    }`}
                  >
                    <div className="text-sm whitespace-pre-wrap break-words leading-relaxed">
                      {stripAnsiCodes(item.content)}
                    </div>
                  </div>
                </div>
              ) : (
                <ToolCallDisplay key={item.id} toolCalls={[{
                  id: item.id,
                  name: item.name,
                  status: item.status,
                  startTime: item.startTime,
                  duration: item.duration,
                  message: item.message,
                  output: item.output,
                  arguments: item.arguments,
                  isComplete: item.status === 'complete' || item.status === 'failed',
                }]} />
              )
            )}
            <div ref={messagesEndRef} />
          </div>
        )}
      </div>
      <div className="bg-white dark:bg-gray-800 border-t border-gray-200 dark:border-gray-700 p-3 md:p-6">
        <div className="max-w-4xl mx-auto">
          <div className="flex gap-2 md:gap-3 items-center">
            <textarea
              ref={inputRef}
              value={inputMessage}
              onChange={(e) => setInputMessage(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  if (inputMessage.trim() && !isSending) {
                    handleSend();
                  }
                } else if (e.key === "ArrowUp") {
                  e.preventDefault();
                  if (history.length === 0) return;

                  if (historyIndex === -1) {
                    setCurrentDraft(inputMessage);
                    setHistoryIndex(history.length - 1);
                    setInputMessage(history[history.length - 1]);
                  } else if (historyIndex > 0) {
                    setHistoryIndex(historyIndex - 1);
                    setInputMessage(history[historyIndex - 1]);
                  }
                } else if (e.key === "ArrowDown") {
                  e.preventDefault();
                  if (historyIndex === -1) return;

                  if (historyIndex < history.length - 1) {
                    setHistoryIndex(historyIndex + 1);
                    setInputMessage(history[historyIndex + 1]);
                  } else {
                    setHistoryIndex(-1);
                    setInputMessage(currentDraft);
                  }
                }
              }}
              placeholder="Type a message..."
              className="flex-1 resize-none rounded-xl border border-gray-300 dark:border-gray-600 px-3 md:px-4 py-3 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent text-gray-900 dark:text-gray-100 bg-white dark:bg-gray-700 placeholder-gray-400 dark:placeholder-gray-500 text-sm md:text-base h-12"
              rows={1}
              disabled={isSending}
            />
            {isSending ? (
              <button
                onClick={handleStop}
                className="bg-red-600 text-white h-12 px-4 md:px-6 rounded-xl hover:bg-red-700 transition-colors font-medium flex items-center justify-center gap-2 text-sm md:text-base flex-shrink-0"
              >
                <Square className="w-4 h-4 fill-white" />
                <span className="hidden md:inline">Stop</span>
              </button>
            ) : (
              <button
                onClick={handleSend}
                disabled={!inputMessage.trim()}
                className="bg-blue-600 text-white h-12 px-4 md:px-6 rounded-xl hover:bg-blue-700 transition-colors font-medium disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2 text-sm md:text-base flex-shrink-0"
              >
                <SendIcon className="w-4 h-4 md:hidden" />
                <span className="hidden md:inline">Send</span>
              </button>
            )}
          </div>
          <div className="mt-2 text-xs text-gray-500 dark:text-gray-400 flex items-center gap-2">
            <span className="w-2 h-2 bg-green-500 rounded-full animate-pulse"></span>
            <span className="truncate">Connected to live session</span>
            {isSending && <span className="ml-2 text-orange-600 dark:text-orange-400 hidden md:inline">Generating response... (ESC to stop)</span>}
          </div>
        </div>
      </div>
      <StatusBar />
    </>
  );
}

function EmptyState() {
  return (
    <div className="flex-1 flex items-center justify-center bg-gradient-to-br from-gray-50 to-gray-100 dark:from-gray-900 dark:to-gray-800">
      <div className="text-center max-w-md px-6">
        <div className="text-6xl mb-6">💬</div>
        <h2 className="text-2xl font-bold text-gray-900 dark:text-gray-100 mb-3">
          Welcome to Inference Gateway UI
        </h2>
        <p className="text-gray-600 dark:text-gray-400">
          Click &quot;New Chat&quot; to start a live session, or select a conversation from the sidebar.
        </p>
      </div>
    </div>
  );
}
