# Task Management Feature

This document describes the A2A task management feature that provides an improved UX for managing background tasks.

## Overview

The task management feature provides a dedicated interface for viewing and managing A2A background
tasks, similar to the conversation selection interface. This provides a comprehensive task management
system with better visibility into task status and history.

## Features

### Main Chat Interface

- Shows a simple task count badge `(N)` in the header when there are active background tasks
- Removes the detailed task display from the main interface for a cleaner look

### Task Management Interface (`/tasks`)

- **Access**: Type `/tasks` in the chat to open the task management interface
- **Requirements**: Only available when A2A is enabled in configuration
- **Default View**: Opens in "All" view showing both active and completed tasks
- **Views**: Switch between Active, Completed, and All tasks using number keys 1, 2, 3

### Keyboard Shortcuts

- `↑/↓` or `j/k`: Navigate between tasks
- `i` or `Enter`: View detailed task information
- `c`: Cancel the selected active task
- `/`: Search tasks by agent name, task ID, or status
- `r`: Refresh task list
- `q` or `Esc`: Exit task management interface

### Task Information

When viewing task details (`i` key), you can see:

- Task ID
- Agent name and URL
- Current status
- Start time and elapsed duration
- Context ID (if available)

### Task Retention

- Completed and canceled tasks are retained **in-memory** for easy reference
- Default retention: 5 tasks (configurable via `a2a.task.completed_task_retention`)
- Tasks are ordered by completion time (most recent first)
- Automatic cleanup when retention limit is exceeded
- **Note**: Retained tasks are lost when the CLI application exits (not persisted to disk)

## Usage

### Opening Task Management

```text
/tasks
```

### Canceling a Task

1. Open task management with `/tasks`
2. Navigate to the task you want to cancel
3. Press `c` to cancel
4. Confirm with `y` or cancel with `n`

### Viewing Task Details

1. Open task management with `/tasks`
2. Navigate to any task
3. Press `i` or `Enter` to view detailed information
4. Press `i`, `Esc`, or `q` to close the details view

### Searching Tasks

1. Open task management with `/tasks`
2. Press `/` to enter search mode
3. Type your search query (searches agent name, task ID, status)
4. Press `Enter` to confirm or `Esc` to cancel search

## Configuration

The task management feature is automatically enabled when A2A is enabled in your configuration:

```yaml
a2a:
  enabled: true
  task:
    completed_task_retention: 5  # Number of completed tasks to retain (default: 5)
  # ... other A2A settings
```

Configuration options:

- `completed_task_retention`: Maximum number of completed/canceled tasks to retain in memory for viewing (default: 5)

If A2A is disabled, the `/tasks` command will show an error message.

## Implementation Details

### Architecture

- **Task Manager**: `TaskManagerImpl` (`internal/ui/components/task_management_view.go`) - Main UI component for task management
- **Task Shortcut**: `A2ATaskManagementShortcut` (`internal/shortcuts/task_management.go`) - Handles the `/tasks` command
- **State Manager**: Manages in-memory task retention using `RetainedTaskInfo` structs
- **Events**: Uses `TasksLoadedEvent` and `TaskCancelledEvent` for state management

### State Management

- Active tasks are loaded from background task polling state in `StateManager`
- Completed tasks are stored in-memory using `RetainedTaskInfo` with automatic retention limits
- Task retention is managed by `StateManager` methods: `AddTaskToInMemoryRetention()`, `GetRetainedTasks()`, etc.
- UI state includes current view (Active/Completed/All), search query, selection, and info display modes

### View States

- `ViewStateA2ATaskManagement`: Dedicated view state for task management
- Proper transitions back to chat view when exiting
- Integration with existing view management system

## Testing

The feature includes comprehensive tests for:

- Task retention service functionality
- Task management shortcut behavior
- Task cancellation and completion flow
- A2A configuration requirements

Run tests with:

```bash
go test ./internal/ui/components/ -v -run TestTaskRetention
go test ./internal/shortcuts/ -v -run TestTaskManagement
```
