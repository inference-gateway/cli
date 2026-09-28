---
name: go-spec-reviewer
description: >
  Review a Go design spec before implementation begins - dispatch a subagent that
  checks a design doc for completeness, consistency, and idiomatic Go (simplicity,
  small consumer-defined interfaces, explicit wrapped errors, context propagation)
  plus Cobra/Viper conventions where applicable. Use when a user has a Go spec to
  review, asks "is this spec ready?", or is about to implement from a written
  design. Distilled from spf13/go-skills and adapted to this repo's conventions.
license: MIT
---

# Go Spec Reviewer

Dispatch a subagent to verify a Go **design spec is complete, consistent, and
idiomatic before implementation begins** - the cheapest place to catch a flawed
design. The reviewer channels Rob Pike, the stdlib authors, and spf13: reject
needless abstraction, demand explicit error handling and context propagation,
expect the simplest design that works. It complements plan mode with a focused
spec gate.

## When to use

- A spec or design doc to review, or the question "is this spec ready?"
- About to implement from a written spec
- A technical review of a planned feature before any code is written

## How to dispatch

Spawn a `general-purpose` subagent (the **Agent** tool) pointed at the spec file,
with the prompt below. No subagent available? Run the same steps inline. It returns
**Status / Issues / Questions / Recommendations** - it does not edit code.

```text
You are a Go spec reviewer. Verify this spec is complete and ready for
implementation, through the lens of idiomatic Go.

Think like Rob Pike: is it simple, doing one thing well?
Think like the stdlib authors: small interfaces, defined at the point of use?
Think like spf13: if it's a CLI, does it follow Cobra/Viper conventions?

Spec to review: <SPEC_FILE_PATH>

Step 0 - Load the standards. Read .agents/skills/go/SKILL.md and
.agents/skills/cobra-viper/SKILL.md first. They define "idiomatic" for this review;
don't re-derive standards that conflict with them.

Step 1 - Codebase context. Before reviewing, explore the repo for conventions and
conflicts: map the bounded contexts under internal/ (AGENTS.md "Architecture") and
the commands under cmd/; commands are NewCommand(...) factories registered in
cmd/root/root.go via AddCommand - a new subcommand must appear in the spec's file
list with that registration point, and must not add package-level command or flag
vars; note existing types/interfaces the spec should reuse rather than reinvent;
note the Go version in go.mod and flag idioms it has obsoleted (third-party routers
where ServeMux suffices, interface{}, loops that slices/maps handle, hand-rolled
worker pools or semaphores).

Step 2 - Go philosophy:
| Concern      | Look for |
| ------------ | -------- |
| Simplicity   | layers/abstractions with one implementation; over-engineering |
| Dependencies | each new third-party dependency justified against a stdlib option? |
| Interfaces   | consumer-defined? small (1-3 methods)? real polymorphism? |
| Errors       | returned explicitly, wrapped with %w, never swallowed? sentinels named where callers branch? |
| Context      | ctx threaded through I/O and long calls? timeouts set? |
| Concurrency  | goroutines with clear ownership and a stated shutdown path? bounded fan-out? races? |
| Packages     | one clear purpose each? new package justified vs extending one? no utils/common? |
| Naming       | short, no stutter (pkg.PkgThing), no Get prefix? |
| Testing      | says how it's tested? fakes at I/O boundaries, table-driven core logic, in-memory CLI runs? |
| YAGNI        | driven by stated requirements, not speculative futures? |

Step 3 - Cobra/Viper (skip if not a CLI):
| Concern      | Look for |
| ------------ | -------- |
| Construction | new commands built by factory functions, not package-level vars? |
| Registration | new subcommands added in cmd/root/root.go via AddCommand? |
| RunE vs Run  | RunE so errors propagate |
| Args         | positional args validated with cobra.Args, not counted inside RunE? |
| Flag scope   | config-level on root (persistent); per-operation on the subcommand |
| Viper        | new keys in the config struct with a default in DefaultConfig()? env names bound? business logic gets typed config, never Viper? |
| Output       | written via cmd.OutOrStdout() so it's testable? |
| Path clash   | --input and --output guarded against resolving to the same path? |

Step 4 - Completeness:
| Category      | Look for |
| ------------- | -------- |
| Completeness  | TODO/TBD/placeholders, missing error paths |
| Consistency   | contradictions, types named differently across sections |
| Clarity       | ambiguity that would make two implementors build different things |
| Scope         | one focused implementation, not several subsystems |
| Data flow     | clear what enters and exits each function or step? |
| Compatibility | changed behavior, flags, config keys or APIs come with a migration/deprecation story? |
| Security      | user input sanitized before shell/path/external use? |

Calibration: only flag issues that would cause real implementation problems - a
missing error path, a flag collision that won't compile, an abstraction that adds
complexity without enabling anything. Skip wording and formatting nits. An unclear
requirement in an otherwise sound spec is a Question, not an Issue. Respect THIS
repo's established conventions (DDD bounded contexts with pure domain/ packages and
depguard-enforced import direction, the DI container as composition root,
counterfeiter-generated mocks, YAML tool manifests, the per-mode bash allow-list) -
judge idiomaticity within them; do not flag the chosen architecture itself as a
defect. Approve unless gaps would lead to a flawed or incomplete implementation.

Output:
## Go Spec Review
Status: Approved | Approved with Questions | Issues Found
Issues (block implementation):
- [Section X]: [specific issue] - [why it matters for implementation]
Questions for Author (need answers, don't block a sound design):
- [Section Y]: [the ambiguity] - [the readings an implementor could take]
Recommendations (advisory, non-blocking):
- [correctness / idiomaticity / clarity suggestions]
```

---

*Adapted from [spf13/go-skills](https://github.com/spf13/go-skills) (MIT, by Steve
Francia) for this repo's local subagent tooling and conventions. Pairs with **go**
and **cobra-viper**.*
