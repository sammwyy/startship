import { useState } from "react";
import { Icon } from "./Icon";
import "./marketplace.css";

const layouts = [
  ["grid", "grid", "Grid"],
  ["cards", "layers", "Cards"],
  ["list", "menu", "List"],
] as const;
type Layout = (typeof layouts)[number][0];

export function useCollectionLayout(collection: "apps" | "services") {
  const key = `startship:${collection}:layout`;
  const [layout, setLayout] = useState<Layout>(() => {
    try {
      const saved = localStorage.getItem(key);
      return layouts.find(([id]) => id === saved)?.[0] ?? "grid";
    } catch {
      return "grid";
    }
  });
  return [
    layout,
    (next: Layout) => {
      setLayout(next);
      try {
        localStorage.setItem(key, next);
      } catch {
        // Keep the active layout even when persistent storage is unavailable.
      }
    },
  ] as const;
}

export function CollectionLayout({
  layout,
  change,
}: {
  layout: Layout;
  change: (layout: Layout) => void;
}) {
  return (
    <div className="collection-switcher" role="group" aria-label="View layout">
      {layouts.map(([id, icon, label]) => (
        <button
          key={id}
          type="button"
          aria-pressed={layout === id}
          onClick={() => change(id)}
        >
          <Icon name={icon} /> {label}
        </button>
      ))}
    </div>
  );
}
