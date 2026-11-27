import type { QueryClient } from "@tanstack/react-query";

export type CatalogPath = "recipes" | "catalog";
export type CatalogMetadata = {
  source: "remote" | "bundled";
  updated_at?: string;
};
export type CatalogSnapshot = {
  entries: unknown[];
  metadata: CatalogMetadata;
};

export function applyCatalogSnapshot(
  client: QueryClient,
  path: CatalogPath,
  snapshot: CatalogSnapshot,
) {
  const queryKey = [path];
  const metadataKey = [`${path}/status`];
  void client.cancelQueries({ queryKey, exact: true });
  void client.cancelQueries({ queryKey: metadataKey, exact: true });
  client.setQueryData(queryKey, snapshot.entries);
  client.setQueryData(metadataKey, snapshot.metadata);
}
