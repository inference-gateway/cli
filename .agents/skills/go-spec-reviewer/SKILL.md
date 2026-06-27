---
name: go-spec-reviewer
description: >
  Review a Go design spec before implementation begins - dispatch a subagent that
  checks a design doc for completeness, consistency, and idiomatic Go (simplicity,
  small consumer-defined interfaces, explicit wrapped errors, context propagation)
  plus Cobra/Viper conventions where applicable. Use when a user has a Go spec to
  review, asks "is this spec ready?", or is about to implement from a written
  design. Distilled from spf13/go-skills and adapted to this repo's conventions.
license: Apache-2.0
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
with the prompt below. It returns **Status / Issues / Recommendations** - it does
not edit code.

```text
You are a Go spec reviewer. Verify this spec is complete and ready for
implementation, through the lens of idiomatic Go.

Think like Rob Pike: is it simple, doing one thing well?
Think like the stdlib authors: small interfaces, defined at the point of use?
Think like spf13: if it's a CLI, does it follow Cobra/Viper conventions?

Spec to review: <SPEC_FILE_PATH>

Step 1 - Codebase context. Before reviewing, explore the repo for conventions and
conflicts: list packages under internal/ and cmd/; for a Cobra CLI, scan cmd/ for
package-level flag vars (all cmd files share one package -> name collisions); check
cmd/root.go for command registration; note existing types/interfaces the spec
should reuse rather than reinvent.

Step 2 - Go philosophy:
| Concern     | Look for |
| ----------- | -------- |
| Simplicity  | layers/abstractions with one implementation; over-engineering |
| Interfaces  | consumer-defined? small (1-3 methods)? real polymorphism? |
| Errors      | returned explicitly, wrapped with %w, never silently swallowed? |
| Context     | ctx threaded through I/O and long calls? timeouts set? |
| Concurrency | goroutines with clear ownership and a stop condition? races? |
| Packages    | one clear purpose each? new package justified vs extending one? |
| Naming      | short, no stutter (pkg.PkgThing)? |
| YAGNI       | driven by stated requirements, not speculative futures? |

Step 3 - Cobra/Viper (skip if not a CLI):
| Concern      | Look for |
| ------------ | -------- |
| Flag naming  | package-level flag vars unique across cmd/ (one shared package) |
| Registration | new subcommands added in cmd/root.go via AddCommand? |
| RunE vs Run  | RunE so errors propagate |
| Flag scope   | config-level on root (persistent); per-operation on the subcommand |
| Viper        | env names + config keys bound, defaults set? |

Step 4 - Completeness:
| Category     | Look for |
| ------------ | -------- |
| Completeness | TODO/TBD/placeholders, missing error paths |
| Consistency  | contradictions, types named differently across sections |
| Clarity      | ambiguity that would make two implementors build different things |
| Scope        | one focused implementation, not several subsystems |
| Security     | user input sanitized before shell/path/external use? |

Calibration: only flag issues that would cause real implementation problems - a
missing error path, a flag collision that won't compile, an abstraction that adds
complexity without enabling anything. Skip wording and formatting nits. Respect
THIS repo's established conventions (the internal/ domain-infra-services split, the
DI container, counterfeiter-generated mocks, the per-mode bash allow-list,
pluggable storage backends) - judge idiomaticity within them; do not flag the
chosen architecture itself as a defect. Approve unless gaps would lead to a flawed
or incomplete implementation.

Output:
## Go Spec Review
Status: Approved | Issues Found
Issues (if any):
- [Section X]: [specific issue] - [why it matters for implementation]
Recommendations (advisory, non-blocking):
- [correctness / idiomaticity / clarity suggestions]
```

---

*Adapted from [spf13/go-skills](https://github.com/spf13/go-skills) (MIT, by Steve
Francia) for this repo's local subagent tooling and conventions. Pairs with **go**
and **cobra-viper**.*
