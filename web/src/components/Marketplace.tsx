import { useState, type ReactNode } from "react";
import { EngineIcon } from "./EngineIcon";
import { Icon } from "./Icon";
import "./marketplace.css";

type Entry = {
  id: string;
  name: string;
  description: string;
  category?: string;
  logo?: string;
};

const categories = new Map([
  ["media", "Media"],
  ["games", "Games"],
  ["communication", "Communication"],
  ["dev-tools", "Developer tools"],
  ["database", "Databases"],
  ["cache", "Caches"],
  ["queue", "Messaging & queues"],
  ["search", "Search"],
  ["storage", "Object storage"],
  ["other", "Other"],
]);

export function categoryLabel(category = "other") {
  return categories.get(category) ?? category;
}

export function Marketplace<T extends Entry>({
  entries,
  kind,
  loading,
  renderTile,
}: {
  entries: T[];
  kind: "app" | "service";
  loading?: boolean;
  renderTile: (entry: T, content: ReactNode) => ReactNode;
}) {
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState("");
  const groups = [
    ...new Set(entries.map((entry) => entry.category || "other")),
  ];
  const category = groups.includes(filter) ? filter : "";
  const matches = entries.filter((entry) => {
    const group = entry.category || "other";
    return (
      (!category || category === group) &&
      `${entry.name} ${categoryLabel(group)} ${entry.description}`
        .toLowerCase()
        .includes(search.trim().toLowerCase())
    );
  });

  return (
    <div className="marketplace">
      <div className="catalog-search">
        <Icon name="search" />
        <input
          aria-label={`Search ${kind} catalog`}
          placeholder={`Search ${kind}s…`}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
        <span>{matches.length} results</span>
      </div>
      <div className="marketplace-filters" role="group" aria-label="Categories">
        {["", ...groups].map((group) => (
          <button
            key={group}
            type="button"
            aria-pressed={category === group}
            onClick={() => setFilter(group)}
          >
            {group ? categoryLabel(group) : "All"}
          </button>
        ))}
      </div>
      {matches.length ? (
        <div className="catalog-grid">
          {matches.map((entry) =>
            renderTile(
              entry,
              <>
                <span className="catalog-tile-top">
                  <EngineIcon
                    engine={entry.logo ?? entry.id}
                    fallback={kind === "app" ? "box" : "database"}
                  />
                  <span className="marketplace-tile-label">
                    <strong>{entry.name}</strong>
                    <span>{categoryLabel(entry.category || "other")}</span>
                  </span>
                  <Icon name="next" />
                </span>
                <span className="catalog-description">{entry.description}</span>
              </>,
            ),
          )}
        </div>
      ) : !loading ? (
        <p className="muted panel-pad">No {kind}s match your filters.</p>
      ) : null}
    </div>
  );
}
