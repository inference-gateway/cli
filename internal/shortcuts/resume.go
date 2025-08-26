package shortcuts

// This file has been temporarily disabled due to import cycle issues.
// Resume functionality has been moved to internal/services/resume_shortcuts.go
// TODO: Refactor the shortcut system to avoid import cycles

/*
Original content was moved to avoid import cycle:
shortcuts -> services -> shortcuts (through mocks/fake files)

The resume functionality is now available in:
internal/services/resume_shortcuts.go

This needs to be integrated properly by refactoring the shortcut system
to break the circular dependency.
*/
