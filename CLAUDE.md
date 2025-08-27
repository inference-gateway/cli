# CLAUDE.md

NEVER use fmt statements for Debugging - instead you use the logger.

NEVER write inline comments.

ALWAYS use table-driven tests for testing your code.

ALWAYS use early returns for handling errors and special cases - use protective programming techniques to avoid deep nesting.

ALWAYS generate mocks for interfaces using counterfeiter - NEVER create custom mocks yourself.

ALWAYS review Taskfile.yml for available developer tasks.

ALWAYS use named imports for nonstandard library packages.

Please refer to AGENTS.md for more information.
