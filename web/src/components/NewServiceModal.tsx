import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { queries } from "../services/queries";
import { request } from "../services/api";
import type { ServiceEngine } from "../services/types";
import { ServiceModal } from "./ServiceUI";
import { ServiceConfig } from "./ServiceConfig";
import { EngineIcon } from "./EngineIcon";
import { ErrorMessage, Loading } from "./ui";
import { Icon } from "./Icon";
import { CatalogRefresh } from "./CatalogRefresh";

export function NewServiceModal({
  workspaceId,
  onClose,
  onSaved,
}: {
  workspaceId: string;
  onClose: () => void;
  onSaved: () => Promise<unknown>;
}) {
  const catalog = useQuery(queries.catalog()),
    [engine, setEngine] = useState<ServiceEngine>(),
    [search, setSearch] = useState("");
  const filtered = (catalog.data || []).filter((e) =>
    `${e.name} ${e.category} ${e.description}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  return (
    <ServiceModal
      title={engine ? `Configure ${engine.name}` : "Deploy a service"}
      description={
        engine
          ? "Make it yours. We’ll handle the container and persistent storage."
          : "Databases, caches and queues. Ready for your next idea."
      }
      onClose={onClose}
      className="catalog-modal"
      eyebrow={engine ? "02 / CONFIGURATION" : "01 / SERVICE CATALOG"}
    >
      {engine ? (
        <ServiceConfig
          key={engine.id}
          engine={engine}
          onBack={() => setEngine(undefined)}
          onSave={async (body) => {
            await request(`workspaces/${workspaceId}/services`, body);
            await onSaved();
            onClose();
          }}
        />
      ) : (
        <>
          <div className="catalog-scroll">
            <CatalogRefresh path="catalog" workspaceId={workspaceId} />
            <div className="catalog-search">
              <Icon name="search" />
              <input
                aria-label="Search service catalog"
                placeholder="Search databases, caches, queues…"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                autoFocus
              />
              <span>{filtered.length} engines</span>
            </div>
            <ErrorMessage error={catalog.error} />
            {catalog.isPending && <Loading />}
            {["database", "cache", "queue", "search", "storage"].map(
              (category) => {
                const engines = filtered.filter((e) => e.category === category);
                return (
                  !!engines.length && (
                    <section className="catalog-category" key={category}>
                      <h3>
                        {
                          (
                            {
                              database: "Databases",
                              cache: "Caches",
                              queue: "Messaging & queues",
                              search: "Search",
                              storage: "Object storage",
                            } as Record<string, string>
                          )[category]
                        }
                      </h3>
                      <div className="catalog-grid">
                        {engines.map((e) => (
                          <button
                            className="catalog-tile"
                            key={e.id}
                            onClick={() => setEngine(e)}
                          >
                            <span className="catalog-tile-top">
                              <EngineIcon engine={e.id} />
                              <strong>{e.name}</strong>
                              <Icon name="next" />
                            </span>
                            <span className="catalog-description">
                              {e.description}
                            </span>
                            <span className="catalog-versions">
                              {e.versions.slice(0, 3).join(" · ")}
                            </span>
                          </button>
                        ))}
                      </div>
                    </section>
                  )
                );
              },
            )}
            {!catalog.isPending && !filtered.length && (
              <p className="panel-pad">
                No services match “{search}”. Try another engine or category.
              </p>
            )}
          </div>
          <div className="service-modal-foot">
            <span className="muted small">
              <Icon name="lock" /> Private by default. Persistent where it
              matters.
            </span>
            <button onClick={onClose}>Cancel</button>
          </div>
        </>
      )}
    </ServiceModal>
  );
}
