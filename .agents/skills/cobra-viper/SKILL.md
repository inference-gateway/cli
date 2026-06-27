---
name: cobra-viper
description: >
  Build and review Go CLI apps with Cobra and Viper (both used heavily here) -
  command-first architecture, RunE error handling, PersistentPreRunE setup, flag
  design, typed-struct config, the flag/env/file/default precedence hierarchy, and
  in-memory command tests. Use when adding commands or flags, wiring Viper config
  or env binding, or reviewing any cmd/ code. Distilled from spf13/go-skills (by
  Cobra's and Viper's author) and adapted to this repo's config layering.
license: Apache-2.0
---

# Go CLI Architecture: Cobra & Viper

Treat the binary as a **router for commands**: Cobra owns flags, args, and
routing; your business logic stays unaware of the CLI and stays testable. Viper is
the single source of truth that merges defaults, config files, env vars, and flags
into one typed config before the app runs.

## Command-first architecture

`cmd/` files do exactly three things: (1) define the command + help, (2) bind its
flags/config, (3) call into a domain package, passing the parsed config and the
command context. Your core packages must have **zero** imports of `cobra` or
`viper`.

```go
package main

import "github.com/you/app/cmd"

func main() { cmd.Execute() } // main.go is this small
```

> **In this repo:** `cmd/` is the thin Cobra layer; all logic lives under
> `internal/` behind a DI **service container** - commands fetch services via
> container accessors, not by importing cobra/viper into business code. The
> surviving rule is "`cmd/` holds no business logic," **not** "avoid `internal/`."

## Cobra essentials

**Use `RunE`, not `Run`** - return errors up the chain instead of `log.Fatal`
(which skips defers). Pass `cmd.Context()` down so `Ctrl+C`/`SIGINT` cancels work.

```go
var serveCmd = &cobra.Command{
    Use:   "serve",
    Short: "Start the server",
    RunE: func(cmd *cobra.Command, args []string) error {
        if err := engine.Run(cmd.Context(), args); err != nil {
            return fmt.Errorf("serve: %w", err)
        }
        return nil
    },
}
```

**Silence noise on runtime errors** so a network timeout doesn't dump usage text:

```go
rootCmd := &cobra.Command{
    Use:           "myapp",
    SilenceUsage:  true, // no help-dump on a runtime failure
    SilenceErrors: true, // main.go prints the error itself
}
```

**`PersistentPreRunE`** on root runs setup (logging, validation) after flags parse
but before any subcommand. Cobra runs it for every subcommand; if a child defines
its own, call the parent's explicitly - Cobra does not chain them.

**Flags:** `PersistentFlags()` for cross-cutting options (config, verbosity,
output); `Flags()` for command-local ones; `MarkFlagRequired` /
`MarkFlagsMutuallyExclusive` for validation; always offer short flags for common
options. Cobra generates shell completion (`myapp completion zsh|bash|fish`) for
free; add `RegisterFlagCompletionFunc` for flag-value completion.

## Viper: merge sources into a typed struct

**Don't** scatter `viper.GetString("db.host")` through business logic - that
couples your domain to Viper and spreads magic strings. Unmarshal once, at the
routing layer, into a typed struct and pass it down.

```go
type Config struct {
    Host string `mapstructure:"host"`
    Port int    `mapstructure:"port"`
}

var cfg Config
if err := viper.Unmarshal(&cfg); err != nil {
    return fmt.Errorf("decoding config: %w", err)
}
```

**Precedence** (highest to lowest): explicit `Set` -> flags (`BindPFlag`) -> env
(`AutomaticEnv`) -> config file -> `SetDefault`. Each source is opt-in; bind env
explicitly for containers, and map nested keys with a replacer:

```go
viper.SetEnvPrefix("myapp")
viper.AutomaticEnv()
viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_")) // serve.addr -> MYAPP_SERVE_ADDR
```

> **In this repo:** config is assembled in `cmd/root.go::initConfig`, layered
> **defaults -> `~/.infer/config.yaml` -> `./.infer/config.yaml` -> flags ->
> `INFER_*` env (env wins)**, and **split across files by concern**
> (`config.yaml`, `prompts.yaml`, `channels.yaml`, `mcp.yaml`, ...). The prefix is
> `INFER`, the replacer is `strings.NewReplacer(".", "_")`, and two list vars
> (`INFER_A2A_AGENTS`, `INFER_TOOLS_BASH_ALLOW_APPEND`) are parsed with
> `parseDelimitedList`. The typed target is the `Config` struct in
> `config/config.go` - extend that; don't sprinkle `viper.Get*` through services.

## Test commands in memory

Cobra commands are structs - test them directly; never shell out to a compiled
binary (`os/exec` is slow, brittle, and hides coverage).

```go
func TestServe(t *testing.T) {
    viper.Reset() // Viper is a global singleton - reset between tests

    buf := new(bytes.Buffer)
    cmd := newServeCmd() // a factory beats a package var for isolation
    cmd.SetOut(buf)
    cmd.SetErr(buf)
    cmd.SetArgs([]string{"--port", "9090"})

    if err := cmd.Execute(); err != nil {
        t.Fatalf("execute: %v", err)
    }
}
```

## Common mistakes

- **Reading Viper too early** - values are empty until `cobra.OnInitialize`
  callbacks run; don't read it in `init()` or `var` blocks.
- **Forgetting `BindPFlag`** - flags aren't visible to Viper until bound.
- **Missing `SetEnvKeyReplacer`** - `serve.addr` won't match `MYAPP_SERVE_ADDR`.
- **Cobra/Viper in business logic** - pass a typed config struct down instead.
- **Racing on global command/Viper state** in parallel tests - use factories +
  `viper.Reset()`.
- **Over-nesting subcommands** - two levels (`app cmd sub`) is usually the limit.

---

*Adapted from [spf13/go-skills](https://github.com/spf13/go-skills) (MIT, by Steve
Francia - author of Cobra and Viper), reconciled with this repo's config layering
and DI container. Pairs with **go** and **go-spec-reviewer**.*
