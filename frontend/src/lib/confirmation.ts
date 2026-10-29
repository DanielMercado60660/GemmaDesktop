export interface ConfirmationOptions {
  title: string;
  description: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
}
export interface PendingConfirmation extends ConfirmationOptions {
  id: number;
}

// One visible question owns one answer. A second request never inherits the
// first request's consent; dismissing or losing its owner always means cancel.
export function createConfirmationBroker() {
  let current: PendingConfirmation | null = null;
  let nextID = 0;
  let resolveCurrent: ((answer: boolean) => void) | undefined;
  const listeners = new Set<() => void>();
  const publish = () => listeners.forEach((listener) => listener());
  function settle(id: number, answer: boolean) {
    if (!current || current.id !== id) return;
    const resolve = resolveCurrent;
    current = null;
    resolveCurrent = undefined;
    publish();
    resolve?.(answer);
  }
  return {
    current: () => current,
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    request(
      options: ConfirmationOptions,
      signal?: AbortSignal,
    ): Promise<boolean> {
      if (current || signal?.aborted) return Promise.resolve(false);
      const id = ++nextID;
      current = { ...options, id };
      const result = new Promise<boolean>((resolve) => {
        resolveCurrent = resolve;
      });
      const abort = () => settle(id, false);
      signal?.addEventListener("abort", abort, { once: true });
      publish();
      return result.finally(() => signal?.removeEventListener("abort", abort));
    },
    settle,
    cancel() {
      if (current) settle(current.id, false);
    },
  };
}

export async function confirmUnchanged<T>(
  snapshot: T,
  isCurrent: (snapshot: T) => boolean,
  ask: () => Promise<boolean>,
) {
  return (await ask()) && isCurrent(snapshot);
}
