"use client";

import { useAgentsStatus, useMCPStatus } from "@/lib/api/client";

export default function StatusBar() {
  const { data: agentsData, isLoading: agentsLoading, error: agentsError } = useAgentsStatus();
  const { data: mcpData, isLoading: mcpLoading, error: mcpError } = useMCPStatus();

  if (agentsLoading && mcpLoading) {
    return null;
  }

  const hasAgents = agentsData && agentsData.total_agents > 0;
  const hasMCP = mcpData && mcpData.total_servers > 0;

  if (!hasAgents && !hasMCP) {
    return null;
  }

  return (
    <div className="flex items-center gap-3 px-3 py-2 text-xs border-t border-border/40 bg-muted/20">
      {hasAgents && !agentsError && (
        <div className="flex items-center gap-1.5">
          <span className="text-muted-foreground">Agents:</span>
          <span className={getAgentStatusColor(agentsData)}>
            {agentsData.ready_agents}/{agentsData.total_agents}
          </span>
        </div>
      )}

      {hasMCP && !mcpError && (
        <div className="flex items-center gap-1.5">
          <span className="text-muted-foreground">🔌</span>
          <span className={getMCPStatusColor(mcpData)}>
            {mcpData.connected_servers}/{mcpData.total_servers}
          </span>
          {mcpData.total_tools > 0 && (
            <span className="text-muted-foreground">
              ({mcpData.total_tools} {mcpData.total_tools === 1 ? "tool" : "tools"})
            </span>
          )}
        </div>
      )}

      {agentsError && (
        <div className="flex items-center gap-1.5 text-destructive">
          <span className="text-muted-foreground">Agents:</span>
          <span>error</span>
        </div>
      )}
      {mcpError && (
        <div className="flex items-center gap-1.5 text-destructive">
          <span className="text-muted-foreground">MCP:</span>
          <span>error</span>
        </div>
      )}
    </div>
  );
}

// Helper function to determine agent status color
function getAgentStatusColor(data: { total_agents: number; ready_agents: number }) {
  if (data.ready_agents === data.total_agents) {
    return "text-green-600 dark:text-green-400 font-medium";
  } else if (data.ready_agents > 0) {
    return "text-yellow-600 dark:text-yellow-400 font-medium";
  } else {
    return "text-muted-foreground font-medium";
  }
}

// Helper function to determine MCP status color
function getMCPStatusColor(data: { total_servers: number; connected_servers: number }) {
  if (data.connected_servers === data.total_servers) {
    return "text-green-600 dark:text-green-400 font-medium";
  } else if (data.connected_servers > 0) {
    return "text-yellow-600 dark:text-yellow-400 font-medium";
  } else {
    return "text-muted-foreground font-medium";
  }
}
