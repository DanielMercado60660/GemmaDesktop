export interface PacketIdentity {
  id: string;
  requestHash: string;
}

// This preflight improves feedback; the host repeats all identity/authority checks.
export function parsePacketResponse(
  text: string,
  packet: PacketIdentity,
): Record<string, unknown> {
  if (text.length > 1_000_000)
    throw new Error("Response exceeds the packet editor limit.");
  const value: unknown = JSON.parse(text);
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Paste a JSON response object.");
  const result = value as Record<string, unknown>;
  if (result.id !== packet.id || result.requestHash !== packet.requestHash)
    throw new Error(
      "This response belongs to a different packet or input revision.",
    );
  const author = result.authoredBy as Record<string, unknown> | undefined;
  if (
    author?.kind !== "codex" ||
    typeof author.identity !== "string" ||
    !author.identity.trim()
  )
    throw new Error(
      "Record the Codex author or independent reviewer identity.",
    );
  return result;
}
