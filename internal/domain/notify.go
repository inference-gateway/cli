package domain

// RetryNotifier, when set, receives a short human-readable notice for each
// SDK-internal HTTP retry (e.g. "⏳ HTTP 502 - retrying in 10s (attempt 2)").
// The headless agent points it at its stdout notification stream so remote
// channels (Telegram) see progress during backoff; the chat TUI leaves it nil.
var RetryNotifier func(message string)
