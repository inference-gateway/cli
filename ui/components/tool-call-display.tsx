"use client";

import { useEffect, useState } from "react";
import type { ToolCallState } from "@/lib/agui-types";
import { Loader2, CheckCircle2, XCircle, Circle, ChevronDown, ChevronRight } from "lucide-react";

interface ToolCallDisplayProps {
  toolCalls: ToolCallState[];
}

function ToolStatusIcon({ status }: { status: ToolCallState['status'] }) {
  switch (status) {
    case 'queued':
      return <Circle className="w-4 h-4 text-gray-400" />;
    case 'running':
      return <Loader2 className="w-4 h-4 text-blue-500 animate-spin" />;
    case 'complete':
      return <CheckCircle2 className="w-4 h-4 text-green-500" />;
    case 'failed':
      return <XCircle className="w-4 h-4 text-red-500" />;
  }
}

function formatDuration(seconds: number): string {
  if (seconds < 1) {
    return `${Math.round(seconds * 1000)}ms`;
  }
  return `${seconds.toFixed(2)}s`;
}

function formatArguments(args: string | undefined): string {
  if (!args) return '';
  try {
    const parsed = JSON.parse(args);
    return JSON.stringify(parsed, null, 2);
  } catch {
    return args;
  }
}

function getArgumentsPreview(args: string | undefined): string {
  if (!args || args === '{}') return '';

  try {
    const parsed = JSON.parse(args);
    const entries = Object.entries(parsed);
    if (entries.length === 0) return '';

    const formatted = entries
      .map(([key, value]) => {
        let valueStr: string;
        if (typeof value === 'string') {
          if (value.length <= 30) {
            valueStr = value;
          } else {
            valueStr = `"${value.substring(0, 30)}..."`;
          }
        } else {
          valueStr = JSON.stringify(value);
        }
        return `${key}=${valueStr}`;
      })
      .join(', ');

    if (formatted.length > 100) {
      return formatted.substring(0, 97) + '...';
    }
    return formatted;
  } catch {
    const trimmed = args.trim();
    if (trimmed.length > 100) {
      return trimmed.substring(0, 97) + '...';
    }
    return trimmed;
  }
}

function getToolSignature(toolName: string, args: string | undefined): string {
  if (!args || args === '{}') {
    return toolName;
  }
  const argsPreview = getArgumentsPreview(args);
  if (!argsPreview) {
    return toolName;
  }
  return `${toolName}(${argsPreview})`;
}

function ToolCallItem({ toolCall }: { toolCall: ToolCallState }) {
  const [elapsed, setElapsed] = useState(0);
  const [isExpanded, setIsExpanded] = useState(false);

  useEffect(() => {
    if (toolCall.status === 'running' && toolCall.startTime) {
      const interval = setInterval(() => {
        setElapsed((Date.now() - toolCall.startTime!) / 1000);
      }, 100);
      return () => clearInterval(interval);
    }
  }, [toolCall.status, toolCall.startTime]);

  const getStatusText = () => {
    switch (toolCall.status) {
      case 'queued':
        return 'queued';
      case 'running':
        return `running ${formatDuration(elapsed)}`;
      case 'complete':
        return toolCall.duration ? `completed in ${formatDuration(toolCall.duration)}` : 'completed';
      case 'failed':
        return toolCall.duration ? `failed after ${formatDuration(toolCall.duration)}` : 'failed';
    }
  };

  const getStatusColor = () => {
    switch (toolCall.status) {
      case 'queued':
        return 'text-gray-600 dark:text-gray-400';
      case 'running':
        return 'text-blue-600 dark:text-blue-400';
      case 'complete':
        return 'text-green-600 dark:text-green-400';
      case 'failed':
        return 'text-red-600 dark:text-red-400';
    }
  };

  const toolSignature = getToolSignature(toolCall.name, toolCall.arguments);

  return (
    <div className="flex flex-col gap-1 py-2 px-3 bg-gray-50 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="flex items-center gap-2 w-full text-left hover:bg-gray-100 dark:hover:bg-gray-700 -mx-3 -my-2 px-3 py-2 rounded-lg transition-colors"
      >
        {isExpanded ? (
          <ChevronDown className="w-4 h-4 text-gray-500 dark:text-gray-400 flex-shrink-0" />
        ) : (
          <ChevronRight className="w-4 h-4 text-gray-500 dark:text-gray-400 flex-shrink-0" />
        )}
        <ToolStatusIcon status={toolCall.status} />
        <div className="flex-1 min-w-0">
          <span className={`font-medium text-sm ${getStatusColor()}`}>
            {toolSignature}
          </span>
          <span className="text-xs text-gray-500 dark:text-gray-400 ml-2">
            ({getStatusText()})
          </span>
        </div>
      </button>

      {isExpanded && (
        <>
          {toolCall.arguments && (
            <div className="ml-10 mt-1">
              <div className="text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">Arguments:</div>
              <div className="p-2 bg-gray-100 dark:bg-gray-900 rounded text-xs font-mono text-gray-800 dark:text-gray-200 overflow-x-auto">
                <pre className="whitespace-pre-wrap break-words">{formatArguments(toolCall.arguments)}</pre>
              </div>
            </div>
          )}

          {toolCall.message && (
            <div className="text-xs text-gray-600 dark:text-gray-400 ml-10 mt-2">
              {toolCall.message}
            </div>
          )}

          {toolCall.output && (
            <div className="ml-10 mt-2">
              <div className="text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">Output:</div>
              <div className="p-2 bg-gray-900 dark:bg-black rounded text-xs font-mono text-gray-100 overflow-x-auto">
                <pre className="whitespace-pre-wrap break-words">{toolCall.output}</pre>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}

export function ToolCallDisplay({ toolCalls }: ToolCallDisplayProps) {
  if (toolCalls.length === 0) {
    return null;
  }

  return (
    <div className="mb-4 space-y-2">
      <div className="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide">
        Tool Execution
      </div>
      {toolCalls.map((toolCall) => (
        <ToolCallItem key={toolCall.id} toolCall={toolCall} />
      ))}
    </div>
  );
}
