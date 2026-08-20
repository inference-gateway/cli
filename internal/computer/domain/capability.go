package domain

// Capability names one thing the computer backend can do on this platform.
type Capability string

const (
	CapabilityPointer  Capability = "pointer"
	CapabilityKeyboard Capability = "keyboard"
	CapabilityScreen   Capability = "screen"
)
