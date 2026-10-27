import { Call, Browser } from "@wailsio/runtime";
type NativeWindow = Window & {
  _wails?: { environment?: unknown };
  wails?: { invoke?: unknown };
  __GEMMA_CALL__?: (method: string, payload: string) => Promise<string>;
};
export function isDesktop() {
  const host = window as NativeWindow;
  return !!(
    host.__GEMMA_CALL__ ||
    host._wails?.environment ||
    host.wails?.invoke ||
    location.protocol === "wails:"
  );
}
export function openExternal(url: string) {
  const parsed = new URL(url);
  if (!["https:", "http:"].includes(parsed.protocol))
    throw new Error("Only HTTP and HTTPS links can be opened.");
  if (isDesktop()) return Browser.OpenURL(parsed.href);
  window.open(parsed.href, "_blank", "noopener,noreferrer");
  return Promise.resolve();
}
export async function api<T = unknown>(
  method: string,
  params: unknown = {},
): Promise<T> {
  const host = window as NativeWindow;
  if (isDesktop()) {
    const raw = host.__GEMMA_CALL__
      ? await host.__GEMMA_CALL__(method, JSON.stringify(params))
      : await Call.ByName(
          import.meta.env.VITE_WAILS_SERVICE ||
            "gemmadesktop.local/app/internal/desktop.Service.Call",
          method,
          JSON.stringify(params),
        );
    return (typeof raw === "string" ? JSON.parse(raw) : raw) as T;
  }
  const response = await fetch("/api/call", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ method, params }),
  });
  const text = await response.text();
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch {
    throw new Error(
      response.ok
        ? "The application returned an unreadable response."
        : "Gemma Desktop backend is unavailable. Open the desktop app or start the development server.",
    );
  }
  if (!response.ok)
    throw new Error(
      typeof value === "object" && value && "error" in value
        ? String((value as { error: unknown }).error)
        : `Request failed (${response.status})`,
    );
  if (
    value &&
    typeof value === "object" &&
    "error" in value &&
    (value as { error: unknown }).error
  )
    throw new Error(String((value as { error: unknown }).error));
  return value as T;
}
