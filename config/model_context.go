package config

// UserContextWindows holds user-configured context window overrides
// (config.yaml `context_windows:` - substring pattern -> tokens), checked
// before the gateway registry. Populated once from the loaded Config in
// cmd/root.go::initConfig. Local servers (llama.cpp -c flag) can have any
// context size, which no built-in heuristic can know.
var UserContextWindows map[string]int
