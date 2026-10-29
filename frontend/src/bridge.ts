import { Call, Browser } from "@wailsio/runtime";
import { decodeApiResponse } from "./lib/apiResponse";
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
  return decodeApiResponse(text, response.status, response.ok) as T;
}
