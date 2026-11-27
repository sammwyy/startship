import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { queries } from "../services/queries";
import { request } from "../services/api";
import type { ServiceEngine } from "../services/types";
import { ServiceModal } from "./ServiceUI";
import { ServiceConfig } from "./ServiceConfig";
import { ErrorMessage, Loading } from "./ui";
import { CatalogRefresh } from "./CatalogRefresh";
import { Marketplace } from "./Marketplace";

export function NewServiceModal({
  workspaceId,
  onClose,
  onSaved,
}: {
  workspaceId: string;
  onClose: () => void;
  onSaved: () => Promise<unknown>;
}) {
  const catalog = useQuery(queries.catalog());
  const [engine, setEngine] = useState<ServiceEngine>();
  return (
    <ServiceModal
      title={engine ? `Configure ${engine.name}` : "Service marketplace"}
      description={
        engine
          ? "Make it yours. We’ll handle the container and persistent storage."
          : undefined
      }
      onClose={onClose}
      className={engine ? "catalog-modal" : "catalog-modal service-marketplace"}
      eyebrow={engine ? "CONFIGURATION" : ""}
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
            <ErrorMessage error={catalog.error} />
            {catalog.isPending && <Loading />}
            <Marketplace
              entries={catalog.data ?? []}
              kind="service"
              loading={catalog.isPending}
              renderTile={(entry, content) => (
                <button
                  type="button"
                  className="catalog-tile"
                  key={entry.id}
                  onClick={() => setEngine(entry)}
                >
                  {content}
                </button>
              )}
            />
          </div>
          <div className="service-marketplace-footer">
            <CatalogRefresh path="catalog" workspaceId={workspaceId} />
          </div>
        </>
      )}
    </ServiceModal>
  );
}
