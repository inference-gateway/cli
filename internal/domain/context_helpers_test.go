package domain

import (
	"context"
	"testing"
)

func TestToolApprovalHelpers(t *testing.T) {
	t.Run("WithToolApproved sets the key to true", func(t *testing.T) {
		ctx := context.Background()
		ctx = WithToolApproved(ctx)

		if !IsToolApproved(ctx) {
			t.Error("Expected IsToolApproved to return true")
		}
	})

	t.Run("IsToolApproved returns false when key not set", func(t *testing.T) {
		ctx := context.Background()

		if IsToolApproved(ctx) {
			t.Error("Expected IsToolApproved to return false for empty context")
		}
	})

	t.Run("IsToolApproved returns false when wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ToolApprovedKey, "not a bool")

		if IsToolApproved(ctx) {
			t.Error("Expected IsToolApproved to return false for wrong type")
		}
	})
}

func TestDirectExecutionHelpers(t *testing.T) {
	t.Run("WithDirectExecution sets the key to true", func(t *testing.T) {
		ctx := context.Background()
		ctx = WithDirectExecution(ctx)

		if !IsDirectExecution(ctx) {
			t.Error("Expected IsDirectExecution to return true")
		}
	})

	t.Run("IsDirectExecution returns false when key not set", func(t *testing.T) {
		ctx := context.Background()

		if IsDirectExecution(ctx) {
			t.Error("Expected IsDirectExecution to return false for empty context")
		}
	})

	t.Run("IsDirectExecution returns false when wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), DirectExecutionKey, "not a bool")

		if IsDirectExecution(ctx) {
			t.Error("Expected IsDirectExecution to return false for wrong type")
		}
	})
}

func TestBashOutputCallbackHelpers(t *testing.T) {
	t.Run("WithBashOutputCallback sets and retrieves callback", func(t *testing.T) {
		ctx := context.Background()
		callbackCalled := false
		callback := func(line string) {
			callbackCalled = true
		}

		ctx = WithBashOutputCallback(ctx, callback)

		retrieved := GetBashOutputCallback(ctx)
		if retrieved == nil {
			t.Fatal("Expected callback to be retrieved")
		}

		retrieved("test")
		if !callbackCalled {
			t.Error("Expected callback to be called")
		}
	})

	t.Run("GetBashOutputCallback returns nil when key not set", func(t *testing.T) {
		ctx := context.Background()

		if GetBashOutputCallback(ctx) != nil {
			t.Error("Expected GetBashOutputCallback to return nil for empty context")
		}
	})

	t.Run("HasBashOutputCallback returns true when set", func(t *testing.T) {
		ctx := context.Background()
		callback := func(line string) {}
		ctx = WithBashOutputCallback(ctx, callback)

		if !HasBashOutputCallback(ctx) {
			t.Error("Expected HasBashOutputCallback to return true")
		}
	})

	t.Run("HasBashOutputCallback returns false when not set", func(t *testing.T) {
		ctx := context.Background()

		if HasBashOutputCallback(ctx) {
			t.Error("Expected HasBashOutputCallback to return false for empty context")
		}
	})
}

func TestBashDetachChannelHelpers(t *testing.T) {
	t.Run("WithBashDetachChannel sets and retrieves channel", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan struct{})
		defer close(ch)

		ctx = WithBashDetachChannel(ctx, ch)

		retrieved := GetBashDetachChannel(ctx)
		if retrieved == nil {
			t.Fatal("Expected channel to be retrieved")
		}
	})

	t.Run("GetBashDetachChannel returns nil when key not set", func(t *testing.T) {
		ctx := context.Background()

		if GetBashDetachChannel(ctx) != nil {
			t.Error("Expected GetBashDetachChannel to return nil for empty context")
		}
	})

	t.Run("HasBashDetachChannel returns true when set", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan struct{})
		defer close(ch)
		ctx = WithBashDetachChannel(ctx, ch)

		if !HasBashDetachChannel(ctx) {
			t.Error("Expected HasBashDetachChannel to return true")
		}
	})

	t.Run("HasBashDetachChannel returns false when not set", func(t *testing.T) {
		ctx := context.Background()

		if HasBashDetachChannel(ctx) {
			t.Error("Expected HasBashDetachChannel to return false for empty context")
		}
	})
}

// Mock implementation of BashDetachChannelHolder for testing
type mockChatHandler struct {
	detachChan chan<- struct{}
}

func (m *mockChatHandler) SetBashDetachChan(ch chan<- struct{}) {
	m.detachChan = ch
}

func (m *mockChatHandler) GetBashDetachChan() chan<- struct{} {
	return m.detachChan
}

func (m *mockChatHandler) ClearBashDetachChan() {
	m.detachChan = nil
}

func TestChatHandlerHelpers(t *testing.T) {

	t.Run("WithChatHandler sets and retrieves handler", func(t *testing.T) {
		ctx := context.Background()
		handler := &mockChatHandler{}

		ctx = WithChatHandler(ctx, handler)

		retrieved := GetChatHandler(ctx)
		if retrieved == nil {
			t.Fatal("Expected handler to be retrieved")
		}
	})

	t.Run("GetChatHandler returns nil when key not set", func(t *testing.T) {
		ctx := context.Background()

		if GetChatHandler(ctx) != nil {
			t.Error("Expected GetChatHandler to return nil for empty context")
		}
	})

	t.Run("HasChatHandler returns true when set", func(t *testing.T) {
		ctx := context.Background()
		handler := &mockChatHandler{}
		ctx = WithChatHandler(ctx, handler)

		if !HasChatHandler(ctx) {
			t.Error("Expected HasChatHandler to return true")
		}
	})

	t.Run("HasChatHandler returns false when not set", func(t *testing.T) {
		ctx := context.Background()

		if HasChatHandler(ctx) {
			t.Error("Expected HasChatHandler to return false for empty context")
		}
	})
}

func TestSessionIDHelpers(t *testing.T) {
	t.Run("WithSessionID sets and retrieves session ID", func(t *testing.T) {
		ctx := context.Background()
		sessionID := "test-session-123"

		ctx = WithSessionID(ctx, sessionID)

		retrieved := GetSessionID(ctx)
		if retrieved != sessionID {
			t.Errorf("Expected session ID %q, got %q", sessionID, retrieved)
		}
	})

	t.Run("GetSessionID returns empty string when key not set", func(t *testing.T) {
		ctx := context.Background()

		if GetSessionID(ctx) != "" {
			t.Error("Expected GetSessionID to return empty string for empty context")
		}
	})

	t.Run("HasSessionID returns true when set", func(t *testing.T) {
		ctx := context.Background()
		ctx = WithSessionID(ctx, "test-session")

		if !HasSessionID(ctx) {
			t.Error("Expected HasSessionID to return true")
		}
	})

	t.Run("HasSessionID returns false when not set", func(t *testing.T) {
		ctx := context.Background()

		if HasSessionID(ctx) {
			t.Error("Expected HasSessionID to return false for empty context")
		}
	})

	t.Run("HasSessionID returns false for empty string", func(t *testing.T) {
		ctx := context.Background()
		ctx = WithSessionID(ctx, "")

		if HasSessionID(ctx) {
			t.Error("Expected HasSessionID to return false for empty string")
		}
	})
}

func TestContextHelperChaining(t *testing.T) {
	t.Run("Multiple helpers can be chained", func(t *testing.T) {
		ctx := context.Background()

		ctx = WithToolApproved(ctx)
		ctx = WithDirectExecution(ctx)
		ctx = WithSessionID(ctx, "session-123")

		if !IsToolApproved(ctx) {
			t.Error("Expected tool to be approved")
		}
		if !IsDirectExecution(ctx) {
			t.Error("Expected direct execution")
		}
		if GetSessionID(ctx) != "session-123" {
			t.Error("Expected session ID to be set")
		}
	})
}
