/**
 * WebSocket client for live chat with headless CLI sessions
 */

export interface ChatMessage {
  role: "user" | "assistant" | "system";
  content: string;
  timestamp: Date;
}

export type ChatEventHandler = (event: ChatEvent) => void;

export interface ChatEvent {
  type: string;
  data?: any;
}

export class WebSocketChatClient {
  private ws: WebSocket | null = null;
  private url: string;
  private sessionId: string | null = null;
  private messageHandlers: Set<ChatEventHandler> = new Set();
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 5;

  constructor(baseURL?: string) {
    const apiURL = baseURL || process.env.NEXT_PUBLIC_API_URL || "http://localhost:8081";
    // Convert http://localhost:8081 to ws://localhost:8081/ws
    this.url = apiURL.replace(/^http/, "ws") + "/ws";
  }

  /**
   * Create a new chat session
   * @param conversationId Optional conversation ID to continue an existing conversation
   * @param sessionId Optional session ID to reuse existing session
   */
  async createSession(conversationId?: string, sessionId?: string): Promise<string> {
    return new Promise((resolve, reject) => {
      let connectionTimeout: NodeJS.Timeout | null = null;
      let isResolved = false;

      const cleanup = () => {
        if (connectionTimeout) {
          clearTimeout(connectionTimeout);
          connectionTimeout = null;
        }
      };

      connectionTimeout = setTimeout(() => {
        if (!isResolved && this.ws && this.ws.readyState !== WebSocket.OPEN) {
          cleanup();
          reject(new Error("Connection timeout"));
        }
      }, 5000);

      this.ws = new WebSocket(this.url);

      this.ws.onopen = () => {
        cleanup();
        this.reconnectAttempts = 0;

        this.send({
          type: "create_session",
          conversation_id: conversationId,
          session_id: sessionId,
        });
      };

      this.ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data);

          if (message.type === "session_created") {
            this.sessionId = message.session_id;
            if (this.sessionId) {
              isResolved = true;
              cleanup();
              resolve(this.sessionId);
            } else {
              cleanup();
              reject(new Error("Session ID not provided"));
            }
            this.notifyHandlers({ type: message.type, data: message });
          } else if (message.type === "error") {
            console.error("WebSocket error:", message.error);
            cleanup();
            reject(new Error(message.error));
          } else {
            this.notifyHandlers({ type: message.type, data: message });
          }
        } catch (error) {
          console.error("Failed to parse WebSocket message:", error);
        }
      };

      this.ws.onerror = (error) => {
        console.error("WebSocket error:", error);
      };

      this.ws.onclose = () => {
        if (!isResolved) {
          if (this.reconnectAttempts === 0) {
            this.reconnectAttempts++;

            setTimeout(() => {
              this.createSession().then(resolve).catch(reject);
            }, 500);
          } else {
            cleanup();
            reject(new Error("WebSocket connection failed"));
          }
        }
      };
    });
  }

  /**
   * Join an existing session
   */
  async joinSession(sessionId: string): Promise<void> {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(this.url);

      this.ws.onopen = () => {
        this.reconnectAttempts = 0;

        this.send({
          type: "join_session",
          session_id: sessionId,
        });
      };

      this.ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data);

          if (message.type === "session_joined") {
            this.sessionId = message.session_id;
            resolve();
          } else if (message.type === "error") {
            console.error("WebSocket error:", message.error);
            reject(new Error(message.error));
          } else {
            this.notifyHandlers({ type: message.type, data: message });
          }
        } catch (error) {
          console.error("Failed to parse WebSocket message:", error);
        }
      };

      this.ws.onerror = (error) => {
        console.error("WebSocket error:", error);
        const errorMessage = error instanceof Error ? error.message : "WebSocket connection failed";
        reject(new Error(errorMessage));
      };

      this.ws.onclose = () => {
        this.handleReconnect();
      };
    });
  }

  /**
   * Send a message to the chat session
   */
  sendMessage(content: string, images: any[] = [], model?: string): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error("WebSocket not connected");
    }

    const message: any = {
      type: "message",
      content,
      images,
    };

    // Include model if specified
    if (model) {
      message.model = model;
    }

    this.send(message);
  }

  /**
   * Send an interrupt signal to stop the current response
   */
  interrupt(): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error("WebSocket not connected");
    }

    this.send({
      type: "interrupt",
    });
  }

  /**
   * Subscribe to chat events
   */
  onMessage(handler: ChatEventHandler): () => void {
    this.messageHandlers.add(handler);

    // Return unsubscribe function
    return () => {
      this.messageHandlers.delete(handler);
    };
  }

  /**
   * Close the WebSocket connection
   */
  close(): void {
    if (this.ws) {
      this.send({ type: "close_session" });
      this.ws.close();
      this.ws = null;
    }
    this.sessionId = null;
  }

  /**
   * Get current session ID
   */
  getSessionId(): string | null {
    return this.sessionId;
  }

  /**
   * Check if connected
   */
  isConnected(): boolean {
    return this.ws !== null && this.ws.readyState === WebSocket.OPEN;
  }

  private send(data: any): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(data));
    }
  }

  private notifyHandlers(event: ChatEvent): void {
    this.messageHandlers.forEach((handler) => {
      try {
        handler(event);
      } catch (error) {
        console.error("Error in message handler:", error);
      }
    });
  }

  private handleReconnect(): void {
    if (this.reconnectAttempts < this.maxReconnectAttempts && this.sessionId) {
      this.reconnectAttempts++;

      setTimeout(() => {
        this.joinSession(this.sessionId!).catch((error) => {
          console.error("Failed to reconnect:", error);
        });
      }, 1000 * Math.pow(2, this.reconnectAttempts));
    }
  }
}
