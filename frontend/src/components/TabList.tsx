import { useId, type ReactNode } from "react";

export interface TabItem<T extends string> {
  id: T;
  label: ReactNode;
  disabled?: boolean;
}

export default function TabList<T extends string>({
  items,
  value,
  onChange,
  label,
  className,
  panelId,
  children,
}: {
  items: TabItem<T>[];
  value: T;
  onChange: (value: T) => void | Promise<void>;
  label: string;
  className?: string;
  panelId?: string;
  children?: ReactNode;
}) {
  const prefix = useId();
  return (
    <div
      role="tablist"
      aria-label={label}
      className={className}
      onKeyDown={(event) => {
        if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key))
          return;
        const buttons = Array.from(
          event.currentTarget.querySelectorAll<HTMLButtonElement>(
            '[role="tab"]:not(:disabled)',
          ),
        );
        const current = buttons.indexOf(
          document.activeElement as HTMLButtonElement,
        );
        if (current < 0 || !buttons.length) return;
        event.preventDefault();
        const next =
          event.key === "Home"
            ? 0
            : event.key === "End"
              ? buttons.length - 1
              : (current +
                  (event.key === "ArrowRight" ? 1 : -1) +
                  buttons.length) %
                buttons.length;
        buttons[next].focus();
        // Manual activation keeps asynchronous discard prompts tied to an explicit
        // Enter/Space/click instead of opening one while arrowing through tabs.
      }}
    >
      {items.map((item) => (
        <button
          key={item.id}
          id={`${prefix}-${item.id}`}
          role="tab"
          aria-selected={value === item.id}
          aria-controls={panelId}
          tabIndex={value === item.id ? 0 : -1}
          disabled={item.disabled}
          className={value === item.id ? "selected" : ""}
          onClick={() => void onChange(item.id)}
        >
          {item.label}
        </button>
      ))}
      {children}
    </div>
  );
}
