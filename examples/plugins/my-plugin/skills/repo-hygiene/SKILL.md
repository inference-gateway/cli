---
name: repo-hygiene
description: Checks a repository for stray scratch files and leftover debug output. Use when tidying a working tree before a commit.
---

# Repo hygiene

1. List untracked and modified files first, and ask which of them should stay.
2. Point out debug prints, TODOs and commented-out code left behind.
3. Suggest the smallest cleanup per file instead of a bulk rewrite.
