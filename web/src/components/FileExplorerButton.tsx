import { lazy, Suspense, useState } from "react";
import { Icon } from "./Icon";

const FileExplorer = lazy(() => import("./FileExplorer"));

export function FileExplorerButton({
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
        aria-label={`Browse files in ${label}`}
        onClick={() => setOpen(true)}
      >
        <Icon name="folder" /> Files
      </button>
      {open && (
        <Suspense fallback={<span role="status">Opening files…</span>}>
          <FileExplorer
            target={target}
            label={label}
            onClose={() => setOpen(false)}
          />
        </Suspense>
      )}
    </>
  );
}
