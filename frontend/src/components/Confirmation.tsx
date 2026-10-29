import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import * as Dialog from "@radix-ui/react-dialog";
import {
  createConfirmationBroker,
  type ConfirmationOptions,
} from "../lib/confirmation";
import { Button } from "./ui";

type Ask = (
  options: ConfirmationOptions,
  signal?: AbortSignal,
) => Promise<boolean>;
const ConfirmationContext = createContext<Ask | null>(null);

export function ConfirmationProvider({ children }: { children: ReactNode }) {
  const [broker] = useState(createConfirmationBroker);
  const pending = useSyncExternalStore(broker.subscribe, broker.current);
  const cancelButton = useRef<HTMLButtonElement>(null);
  const previousFocus = useRef<HTMLElement | null>(null);
  const ask = useCallback<Ask>(
    (options, signal) => {
      if (
        !broker.current() &&
        document.activeElement instanceof HTMLElement &&
        !document.activeElement.closest(".confirmation-dialog")
      )
        previousFocus.current = document.activeElement;
      return broker.request(options, signal);
    },
    [broker],
  );
  useEffect(() => () => broker.cancel(), [broker]);
  return (
    <ConfirmationContext.Provider value={ask}>
      {children}
      <Dialog.Root
        key={pending?.id || "closed"}
        open={!!pending}
        onOpenChange={(open) => {
          if (!open) broker.cancel();
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="modal-overlay confirmation-overlay" />
          <Dialog.Content
            className="modal confirmation-dialog"
            role="alertdialog"
            onOpenAutoFocus={(event) => {
              event.preventDefault();
              cancelButton.current?.focus();
            }}
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              if (previousFocus.current?.isConnected)
                previousFocus.current.focus();
            }}
          >
            <div className="modal-heading">
              <div>
                <Dialog.Title>{pending?.title}</Dialog.Title>
                <Dialog.Description>{pending?.description}</Dialog.Description>
              </div>
            </div>
            <div className="modal-actions">
              <Button ref={cancelButton} onClick={() => broker.cancel()}>
                {pending?.cancelLabel || "Cancel"}
              </Button>
              <Button
                variant={pending?.danger ? "danger" : "default"}
                onClick={() => {
                  if (pending) broker.settle(pending.id, true);
                }}
              >
                {pending?.confirmLabel || "Confirm"}
              </Button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </ConfirmationContext.Provider>
  );
}

export function useConfirmation() {
  const ask = useContext(ConfirmationContext);
  if (!ask) throw new Error("ConfirmationProvider is required.");
  const lifetime = useRef(new AbortController());
  useEffect(() => {
    if (lifetime.current.signal.aborted)
      lifetime.current = new AbortController();
    return () => lifetime.current.abort();
  }, []);
  return useCallback(
    (options: ConfirmationOptions) => ask(options, lifetime.current.signal),
    [ask],
  );
}
