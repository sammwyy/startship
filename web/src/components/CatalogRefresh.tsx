import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "../services/api";
import {
  applyCatalogSnapshot,
  type CatalogMetadata,
  type CatalogPath,
  type CatalogSnapshot,
} from "../services/catalogs";
import { ErrorMessage } from "./ui";

export function CatalogRefresh({
  path,
  workspaceId,
}: {
  path: CatalogPath;
  workspaceId: string;
}) {
  const client = useQueryClient();
  const metadata = useQuery({
    queryKey: [`${path}/status`],
    queryFn: ({ signal }) =>
      request<CatalogMetadata>(`${path}/status`, undefined, "GET", signal),
  });
  const refresh = useMutation({
    mutationFn: () =>
      request<CatalogSnapshot>(
        `${path}/refresh?workspace=${encodeURIComponent(workspaceId)}`,
        undefined,
        "POST",
      ),
    onSuccess: (snapshot) => applyCatalogSnapshot(client, path, snapshot),
  });
  const updated = metadata.data?.updated_at;
  return (
    <>
      <div className="catalog-refresh">
        <span className="small muted">
          {metadata.data?.source === "remote"
            ? "Remote cache"
            : metadata.data?.source === "bundled"
              ? "Bundled catalog"
              : "Catalog source unavailable"}
          {updated && (
            <>
              {" "}
              · Last refreshed{" "}
              <time dateTime={updated}>
                {new Date(updated).toLocaleString()}
              </time>
            </>
          )}
        </span>
        <button disabled={refresh.isPending} onClick={() => refresh.mutate()}>
          {refresh.isPending ? "Refreshing…" : "Refresh catalog"}
        </button>
      </div>
      <ErrorMessage error={refresh.error || metadata.error} />
    </>
  );
}
