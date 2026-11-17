import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent, ReactNode } from "react";

export function Popover({
  id,
  label,
  className,
  trigger,
  children,
}: {
  id: string;
  label: string;
  className?: string;
  trigger: ReactNode;
  children: ReactNode;
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
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    menu.current?.showPopover();
    const items = Array.from(
      menu.current!.querySelectorAll<HTMLElement>("a,button"),
    );
    const index = items.indexOf(document.activeElement as HTMLElement);
    const target =
      items[
        event.key === "Home"
          ? 0
          : event.key === "End"
            ? items.length - 1
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
        popoverTarget={id}
        onKeyDown={keyboard}
      >
        {trigger}
      </button>
      <div
        id={id}
        ref={menu}
        className="sidebar-menu"
        popover="auto"
        onKeyDown={keyboard}
        onClick={(event) => {
          if ((event.target as HTMLElement).closest("a,button"))
            menu.current?.hidePopover();
        }}
      >
        {children}
      </div>
    </>
  );
}
