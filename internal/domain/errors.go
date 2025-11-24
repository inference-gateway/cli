package domain

import (
	"fmt"
	"runtime"
)

// PortCollisionError represents an error when a port is already in use
type PortCollisionError struct {
	Port    string
	Service string // "gateway" or agent name
}

// Error implements the error interface
func (e *PortCollisionError) Error() string {
	var resolutionSteps string

	if e.Service == "gateway" {
		resolutionSteps = fmt.Sprintf(`✗ Error: Port %s is already in use

  The gateway cannot start because port %s is already in use by another process.

  To resolve this:
  1. Find the process using the port:
     %s

  2. Stop the conflicting process, or

  3. Configure the gateway to use a different port in .infer/config.yaml:
     gateway:
       url: http://localhost:<new-port>`, e.Port, e.Port, getPortCheckCommand(e.Port))
	} else {
		resolutionSteps = fmt.Sprintf(`✗ Error: Port %s is already in use

  Agent '%s' cannot start because port %s is already in use.

  To resolve this:
  1. Stop the process using port %s:
     %s

  2. Or update the agent URL to use a different port:
     infer agents update %s --url http://localhost:<new-port>`, e.Port, e.Service, e.Port, e.Port, getPortCheckCommand(e.Port), e.Service)
	}

	return resolutionSteps
}

// getPortCheckCommand returns the platform-specific command to check port usage
func getPortCheckCommand(port string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("netstat -ano | findstr :%s", port)
	}
	return fmt.Sprintf("lsof -ti:%s", port)
}
