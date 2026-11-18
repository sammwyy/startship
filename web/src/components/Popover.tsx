import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent, ReactNode } from "react";

export function Popover({
  id,
  label,
  className,
  trigger,
  children,
  menuRole,
  menuClassName,
  disabled,
}: {
  id: string;
  label: string;
  className?: string;
  trigger: ReactNode;
  children: ReactNode;
  menuRole?: "listbox";
  menuClassName?: string;
  disabled?: boolean;
}) {
  const button = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const node = menu.current!;
    const sync = () => {
      const visible = node.matches(":popover-open");
      setOpen(visible);
      if (!visible || !button.current) return;
      node.style.maxHeight = "";
      const rect = button.current.getBoundingClientRect(),
        below = innerHeight - rect.bottom - 12,
        above = rect.top - 12;
      node.style.left = `${Math.max(8, Math.min(rect.left, innerWidth - node.offsetWidth - 8))}px`;
      node.style.top = `${below >= node.offsetHeight || below >= above ? rect.bottom + 6 : Math.max(8, rect.top - node.offsetHeight - 6)}px`;
      node.style.maxHeight = `${Math.max(80, below >= node.offsetHeight || below >= above ? below : above)}px`;
    };
    node.addEventListener("toggle", sync);
    return () => node.removeEventListener("toggle", sync);
  }, []);
  function keyboard(event: KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      menu.current?.hidePopover?.();
      button.current?.focus();
      return;
    }
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    menu.current?.showPopover();
    const items = Array.from(
      menu.current!.querySelectorAll<HTMLElement>("a,button:not(:disabled)"),
    );
    const index = items.indexOf(document.activeElement as HTMLElement);
    const target =
      items[
        event.key === "Home"
          ? 0
          : event.key === "End"
            ? items.length - 1
            : index < 0
              ? event.key === "ArrowUp"
                ? items.length - 1
                : 0
              : (index + (event.key === "ArrowDown" ? 1 : items.length - 1)) %
                items.length
      ];
    target?.focus({ preventScroll: true });
    target?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }
  return (
    <>
      <button
        ref={button}
        type="button"
        className={className}
        aria-label={label}
        title={label}
        aria-expanded={open}
        aria-haspopup={menuRole}
        aria-controls={id}
        disabled={disabled}
        popoverTarget={id}
        onKeyDown={keyboard}
      >
        {trigger}
      </button>
      <div
        id={id}
        ref={menu}
        className={`sidebar-menu ${menuClassName || ""}`}
        role={menuRole}
        aria-label={menuRole ? label : undefined}
        popover="auto"
        onKeyDown={keyboard}
        onClick={(event) => {
          if (
            (event.target as HTMLElement).closest("a,button:not(:disabled)")
          ) {
            menu.current?.hidePopover?.();
            if (menuRole) button.current?.focus();
          }
        }}
      >
        {children}
      </div>
    </>
  );
}
