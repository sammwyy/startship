import { lazy, Suspense, useState } from "react";
import { Icon } from "./Icon";

const TerminalDialog = lazy(() => import("./TerminalDialog"));

export function TerminalButton({
  target,
  label,
}: {
  target: string;
  label: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        className="button"
        aria-label={`Open terminal for ${label}`}
        aria-haspopup="dialog"
        onClick={() => setOpen(true)}
      >
        <Icon name="terminal" /> Terminal
      </button>
      {open && (
        <Suspense fallback={<span role="status">Opening terminal…</span>}>
          <TerminalDialog
            target={target}
            label={label}
            onClose={() => setOpen(false)}
          />
        </Suspense>
      )}
    </>
  );
}
