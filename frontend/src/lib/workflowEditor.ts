import type { Node } from "@xyflow/react";
import type { WorkflowNode } from "../types";

// The backend also persists fan-out items. Keep that wire field here until the
// shared workflow types include it; editor-only JSON must never be persisted.
type StoredWorkflowNode = WorkflowNode & { items?: string[] };

export function workflowEditorNodes(nodes: StoredWorkflowNode[]): Node[] {
  return nodes.map((node) => ({
    id: node.id,
    position: { ...node.position },
    type: "default",
    data: {
      ...node,
      items: node.items ? [...node.items] : undefined,
      itemsJSON:
        node.items === undefined ? "" : JSON.stringify(node.items, null, 2),
    },
    className: `flow-node node-${node.type}`,
  }));
}

export function workflowSaveNodes(nodes: Node[]): StoredWorkflowNode[] {
  return nodes.map((node) => {
    let items: string[] | undefined;
    const source = String(node.data.itemsJSON ?? "").trim();
    if (source) {
      let parsed: unknown;
      try {
        parsed = JSON.parse(source);
      } catch {
        throw new Error(
          `Step “${node.data.label || node.id}”: items must be a JSON array of strings.`,
        );
      }
      if (
        !Array.isArray(parsed) ||
        parsed.some((item) => typeof item !== "string") ||
        parsed.length > 16
      ) {
        throw new Error(
          `Step “${node.data.label || node.id}”: items must contain at most 16 strings.`,
        );
      }
      items = parsed;
    }
    return {
      id: node.id,
      type: String(node.data.type || "agent"),
      label: String(node.data.label || ""),
      agentId: node.data.agentId ? String(node.data.agentId) : undefined,
      prompt: node.data.prompt ? String(node.data.prompt) : undefined,
      condition: node.data.condition ? String(node.data.condition) : undefined,
      position: { ...node.position },
      ...(items === undefined ? {} : { items }),
    };
  });
}
