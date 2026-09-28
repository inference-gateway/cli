---
name: cobra-viper
description: >
  Build and review Go CLI apps with Cobra and Viper (both used heavily here) -
  command factories, RunE error handling, Args validation, PersistentPreRunE setup,
  flag design, typed-struct config, the flag/env/file/default precedence hierarchy,
  and in-memory command tests. Use when adding or reviewing commands, subcommands,
  flags, Viper config or env binding, or any cmd/ code - even if Cobra or Viper
  isn't named. Distilled from spf13/go-skills (by Cobra's and Viper's author) and
  adapted to this repo's config layering.
license: MIT
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

**Commands are built, not declared.** Construct the tree with factory functions,
never package-level `var` commands - globals leak flag state between tests and make
the tree unusable as a library. The root factory owns a `viper.New()` instance and
injects it:

```go
func NewRootCmd() *cobra.Command {
    v := viper.New()
    root := &cobra.Command{
        Use:           "myapp",
        SilenceUsage:  true, // no help-dump on a runtime failure
        SilenceErrors: true, // main prints the error once
        PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
            return initConfig(v, cmd)
        },
    }
    root.AddCommand(NewServeCmd(v))
    return root
}

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    if err := NewRootCmd().ExecuteContext(ctx); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

`cmd.Context()` is only canceled on Ctrl+C if `main` wires signals like this -
Cobra doesn't do it for you.

> **In this repo:** `cmd/root/root.go` `NewCommand()` builds the tree; every
> subcommand is a `<pkg>.NewCommand(state, ...)` factory taking the shared
> `*runtime.State` (`cmd/runtime/state.go`), which owns the `viper.New()` instance
> and the loaded `*config.Config`. `root.Execute()` runs it through
> `fang.Execute`, which sets `SilenceUsage`/`SilenceErrors` and calls
> `ExecuteContext`; it installs no signal handler (no `fang.WithNotifySignal`), so
> long-running commands (`daemon`, the web server) handle signals themselves.
> Business logic lives under `internal/` behind the DI container - `cmd/` holds
> no business logic.

## Cobra essentials

**Use `RunE`, not `Run`** - return errors up the chain instead of `log.Fatal`
(which skips defers). Pass `cmd.Context()` down.

```go
func NewServeCmd(v *viper.Viper) *cobra.Command {
    cmd := &cobra.Command{
        Use:  "serve [addr]",
        Args: cobra.MaximumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            var cfg engine.Config
            if err := v.Unmarshal(&cfg); err != nil {
                return fmt.Errorf("decoding config: %w", err)
            }
            return engine.Serve(cmd.Context(), cfg)
        },
    }
    cmd.Flags().String("log-level", "info", "log level")
    return cmd
}
```

**Validate positional args with `Args`**, never by counting inside `RunE`:
`cobra.NoArgs`, `ExactArgs(n)`, `MinimumNArgs(n)`, `RangeArgs(min, max)`,
`OnlyValidArgs` (with `ValidArgs`), combined via `cobra.MatchAll(...)`.

**`PersistentPreRunE`** on root runs setup (config, logging) after flags parse but
before any subcommand. If a child defines its own, it *replaces* the parent's -
call the parent explicitly, or opt into chaining with
`cobra.EnableTraverseRunHooks = true` (Cobra 1.8+).

**Flags:** `PersistentFlags()` for cross-cutting options (config, verbosity,
output); `Flags()` for command-local ones; `BoolP`/`StringP` short flags for common
options. Declare relationships instead of checking them by hand:
`MarkFlagRequired`, `MarkFlagsMutuallyExclusive`, `MarkFlagsRequiredTogether`,
`MarkFlagsOneRequired` (1.8+).

**Help and completion:** group many subcommands with `AddGroup` + `GroupID`
(1.6+). Completion (`myapp completion zsh|bash|fish`) is free; add
`RegisterFlagCompletionFunc` for flag values and `ValidArgsFunction` for
positional args.

**Print through the command** - `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` (or
`cmd.Println`). `fmt.Printf` bypasses `SetOut`/`SetErr`, so tests can't capture it.

**Version:** set the root's `Version` field (`myapp --version` for free,
`SetVersionTemplate` to format it); add a `version` subcommand only for structured
output like `--json`.

## Viper: merge sources into a typed struct

Prefer an injected `*viper.Viper` from `viper.New()` over the global singleton - it
isolates tests and lets commands run concurrently. **Don't** scatter
`v.GetString("db.host")` through business logic; unmarshal once, at the routing
layer, into a typed struct and pass it down.

**Precedence** (highest to lowest): explicit `Set` -> flags (`BindPFlags`) -> env
(`AutomaticEnv`) -> config file -> `SetDefault`. Bind the command's whole flag set
in `PersistentPreRunE` (`v.BindPFlags(cmd.Flags())`), not flag-by-flag in `init()`
where two commands binding the same key means the last `init()` silently wins.

```go
func initConfig(v *viper.Viper, cmd *cobra.Command) error {
    v.SetEnvPrefix("myapp")
    v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_")) // serve.addr -> MYAPP_SERVE_ADDR
    v.AutomaticEnv()
    if err := v.ReadInConfig(); err != nil {
        var notFound viper.ConfigFileNotFoundError
        if !errors.As(err, &notFound) {
            return fmt.Errorf("reading config: %w", err)
        }
    }
    return v.BindPFlags(cmd.Flags())
}
```

**The most common Viper bug:** `Unmarshal` only walks keys Viper already knows
(defaults, config file, explicit binds). A value set *only* by env var is invisible
to it unless the key is registered - `SetDefault` or `BindEnv` every key in the
config struct.

> **In this repo:** config is assembled in `runtime.State.Initialize`
> (`cmd/runtime/state.go`) and `loadConfigFromViper` (`cmd/runtime/config.go`),
> layered **defaults -> `~/.infer/config.yaml` -> `./.infer/config.yaml` -> flags
> -> `INFER_*` env (env wins)**, and **split across files by concern**
> (`config.yaml`, `prompts.yaml`, `channels.yaml`, `mcp.yaml`, ...). The prefix is
> `INFER`, the replacer is `strings.NewReplacer(".", "_")`. The gotcha above is
> handled twice: `registerConfigDefaults` (`cmd/runtime/defaults.go`) registers
> every non-zero leaf of `config.DefaultConfig()`, and
> `resolveViperEnvironmentVariables` (`cmd/runtime/config.go`) walks the struct
> after `Unmarshal` to apply `INFER_*` overrides to the rest. Two list vars
> (`INFER_A2A_AGENTS`, `INFER_TOOLS_BASH_ALLOW_APPEND`) are parsed with
> `parseDelimitedList`. The typed target is the `Config` struct in
> `config/config.go` - extend that (and `DefaultConfig()`); don't sprinkle
> `viper.Get*` through services.

## Test commands in memory

Cobra commands are structs - test them in process; never shell out to a compiled
binary (`os/exec` is slow, brittle, and hides coverage). **Always execute through
a fresh root** with the subcommand name in `SetArgs`: `Execute()` on a subcommand
runs from the root anyway, and a fresh tree + fresh Viper per case means no shared
state and no `viper.Reset()`.

```go
func TestServe(t *testing.T) {
    tests := []struct {
        name    string
        args    []string
        wantErr bool
    }{
        {"defaults", []string{"serve"}, false},
        {"bad flag", []string{"serve", "--bogus"}, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Setenv("MYAPP_LOG_LEVEL", "debug") // restored automatically
            buf := new(bytes.Buffer)
            root := NewRootCmd()
            root.SetOut(buf)
            root.SetErr(buf)
            root.SetArgs(tt.args)
            if err := root.ExecuteContext(t.Context()); (err != nil) != tt.wantErr {
                t.Fatalf("execute: err = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

## Common mistakes

- **Executing a subcommand variable directly** - build a fresh tree and drive it
  through `SetArgs`.
- **Env-only values missing after `Unmarshal`** - register every key with
  `SetDefault`/`BindEnv`.
- **Reading Viper too early** - values are empty until setup runs
  (`PersistentPreRunE`); don't read it in `init()` or `var` blocks.
- **Forgetting `BindPFlags`** - flags aren't visible to Viper until bound.
- **Missing `SetEnvKeyReplacer`** - `serve.addr` won't match `MYAPP_SERVE_ADDR`.
- **Cobra/Viper in business logic** - pass a typed config struct down instead.
- **`fmt.Printf` inside commands** - breaks output capture; use `cmd.OutOrStdout()`.
- **Over-nesting subcommands** - two levels (`app cmd sub`) is usually the limit.

---

*Adapted from [spf13/go-skills](https://github.com/spf13/go-skills) (MIT, by Steve
Francia - author of Cobra and Viper), reconciled with this repo's config layering
and DI container. Pairs with **go** and **go-spec-reviewer**.*
