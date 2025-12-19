package services

// GetAllAgentNames returns names of all agents (local and external)
func (am *AgentManager) GetAllAgentNames() []string {
	am.containersMutex.Lock()
	defer am.containersMutex.Unlock()

	names := make([]string, 0, len(am.containers)+len(am.externalAgents))

	for name := range am.containers {
		names = append(names, name)
	}

	for name := range am.externalAgents {
		names = append(names, name)
	}

	return names
}

// GetTotalAgentCount returns the total number of agents (local + external)
func (am *AgentManager) GetTotalAgentCount() int {
	am.containersMutex.Lock()
	defer am.containersMutex.Unlock()

	localCount := len(am.containers)
	externalCount := len(am.externalAgents)

	return localCount + externalCount
}

// IsExternalAgent returns true if the agent is external (not managed by this CLI)
func (am *AgentManager) IsExternalAgent(agentName string) bool {
	am.containersMutex.Lock()
	defer am.containersMutex.Unlock()

	_, isExternal := am.externalAgents[agentName]
	return isExternal
}

// GetExternalAgentURL returns the URL for an external agent
func (am *AgentManager) GetExternalAgentURL(agentName string) string {
	am.containersMutex.Lock()
	defer am.containersMutex.Unlock()

	return am.externalAgents[agentName]
}
