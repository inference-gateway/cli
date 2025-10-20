# Task Management Feature

This document describes the A2A task management feature that provides an improved UX for managing background tasks.

## Overview

The task management feature provides a dedicated interface for viewing and managing A2A background
tasks, similar to the conversation selection interface. This replaces the basic `/cancel <task index>`
command with a more comprehensive task management system.

## Features

### Main Chat Interface

- Shows a simple task count badge `(N)` in the header when there are active background tasks
- Removes the detailed task display from the main interface for a cleaner look

### Task Management Interface (`/tasks`)

- **Access**: Type `/tasks` in the chat to open the task management interface
- **Requirements**: Only available when A2A is enabled in configuration
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

- Completed and canceled tasks are retained for easy reference
- Default retention: 5 tasks (configurable)
- Tasks are ordered by completion time (most recent first)
- Automatic cleanup when retention limit is exceeded

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
  # ... other A2A settings
```

If A2A is disabled, the `/tasks` command will show an error message.

## Implementation Details

### Architecture

- **Task Manager Component**: `TaskManagerImpl` - Main UI component for task management
- **Task Retention Service**: `SimpleTaskRetentionService` - Manages completed task history
- **Task Shortcut**: `TaskManagementShortcut` - Handles the `/tasks` command
- **Events**: Uses `TasksLoadedEvent` and `TaskCancelledEvent` for state management

### State Management

- Active tasks are loaded from the current state manager
- Completed tasks are stored in memory with automatic retention
- UI state includes current view (Active/Completed/All), search, and selection

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
