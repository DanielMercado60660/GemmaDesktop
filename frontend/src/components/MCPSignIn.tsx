import { useEffect, useState } from "react";
import { api } from "../bridge";
import type { Action } from "../Management";
import { Button } from "./ui";

interface AuthState {
  serverId: string;
  status: string;
  error?: string;
}
export default function MCPSignIn({
  id,
  action,
}: {
  id: string;
  action: Action;
}) {
  const [auth, setAuth] = useState<AuthState>(),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [refresh, setRefresh] = useState(0);
  useEffect(() => {
    let valid = true;
    let pending = false;
    async function load() {
      if (pending) return;
      pending = true;
      try {
        const value = await api<AuthState>("mcp.oauth.get", { id });
        if (valid) {
          setAuth(value);
          setError("");
        }
      } catch (e) {
        if (valid) setError(String(e));
      } finally {
        pending = false;
      }
    }
    void load();
    const timer = window.setInterval(load, 2500);
    return () => {
      valid = false;
      window.clearInterval(timer);
    };
  }, [id, refresh]);
  async function call(method: string) {
    setBusy(true);
    try {
      const value = await action<AuthState>(method, { id });
      if (value) {
        setAuth(value);
        setRefresh((v) => v + 1);
      }
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="mcp-auth-controls">
      <div className="row-actions">
        <span className="small muted">
          {auth?.status === "connected"
            ? "Signed in"
            : auth?.status === "pending"
              ? "Waiting for browser sign-in"
              : "Sign-in required"}
        </span>
        <Button
          size="small"
          loading={busy}
          disabled={auth?.status === "pending"}
          onClick={() => call("mcp.oauth.start")}
        >
          {auth?.status === "connected"
            ? "Sign in again"
            : "Sign in with browser"}
        </Button>
        {(auth?.status === "connected" || auth?.status === "pending") && (
          <Button size="small" onClick={() => call("mcp.oauth.signout")}>
            {auth.status === "pending" ? "Cancel sign-in" : "Sign out"}
          </Button>
        )}
      </div>
      {(error || auth?.error) && (
        <p className="error-text">{error || auth?.error}</p>
      )}
    </div>
  );
}
