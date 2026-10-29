export function decodeApiResponse(
  text: string,
  status: number,
  ok: boolean,
): unknown {
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch {
    if (ok) throw new Error("The application returned an unreadable response.");
    const detail = text.trim().startsWith("<") ? "" : text.trim().slice(0, 240);
    throw new Error(
      `Request failed (${status})${detail ? `: ${detail}` : ". The application backend may be unavailable."}`,
    );
  }
  if (!ok) {
    const error =
      value && typeof value === "object" && "error" in value
        ? String(value.error)
        : `Request failed (${status})`;
    throw new Error(error);
  }
  // Successful domain entities may themselves contain an error field, such as
  // an interrupted run. Only the transport status identifies a failed call.
  return value;
}
