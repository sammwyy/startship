import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ListenAddressPicker } from "../components/ListenAddressPicker";
import { ErrorMessage, Loading } from "../components/ui";
import { useLiveSubscription } from "../providers/LiveProvider";
import { useSession } from "../providers/SessionProvider";
import { request } from "../services/api";
import { queries } from "../services/queries";
import type { RouterConfig, RouterSettings } from "../services/types";

const listenFields = [
  ["http_listen", "HTTP listen addresses", "80"],
  ["https_listen", "HTTPS listen addresses", "443"],
] as const;

type RouterDraft = Partial<{
  enabled: boolean;
  http_listen: string;
  https_listen: string;
}>;

export function RouterPanel() {
  const { user } = useSession();
  const admin = !!user?.platform_admin;
  const client = useQueryClient();
  const query = queries.router();
  const status = useQuery(query);
  const interfacesQuery = useQuery(queries.interfaces());
  useLiveSubscription({ topic: "system_router" });
  const [draft, setDraft] = useState<RouterDraft>({});
  const save = useMutation({
    mutationFn: (body: RouterSettings) =>
      request<RouterConfig>("system/router", body, "PUT"),
    onSuccess: (data) => {
      void client.cancelQueries({ queryKey: query.queryKey, exact: true });
      client.setQueryData(query.queryKey, data);
      setDraft({});
    },
  });
  const cfg = status.data;
  const interfaces = interfacesQuery.data?.interfaces ?? [];
  const disabledOverridden = cfg?.overridden.includes("router_disabled");
  const enabled =
    (admin && !disabledOverridden ? draft.enabled : undefined) ??
    cfg?.enabled ??
    false;
  const value = (field: (typeof listenFields)[number][0]) =>
    (admin ? draft[field] : undefined) ?? cfg?.[field].join(", ") ?? "";
  const addresses = (field: (typeof listenFields)[number][0]) =>
    value(field)
      .split(",")
      .map((entry) => entry.trim())
      .filter(Boolean);
  const changed =
    !!cfg &&
    (enabled !== cfg.enabled ||
      listenFields.some(
        ([field]) => addresses(field).join(",") !== cfg[field].join(","),
      ));
  const edit = (value: RouterDraft) => {
    setDraft((prev) => ({ ...prev, ...value }));
    if (save.isError) save.reset();
  };

  return (
    <section className="panel panel-pad" aria-labelledby="router-title">
      <div className="section-head">
        <div>
          <h2 id="router-title">Router</h2>
          <p className="small muted">
            Public HTTP and HTTPS for application domains.
          </p>
        </div>
        {cfg && (
          <span
            role="status"
            className={`badge${cfg.enabled ? " running" : ""}`}
          >
            {cfg.enabled ? "Enabled" : "Disabled"}
          </span>
        )}
      </div>
      {status.isPending && <Loading />}
      <ErrorMessage error={status.error} />
      {cfg && (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (admin && changed && !save.isPending) {
              save.mutate({
                enabled,
                http_listen: addresses("http_listen"),
                https_listen: addresses("https_listen"),
              });
            }
          }}
        >
          {admin && (
            <label className="router-toggle">
              <input
                type="checkbox"
                role="switch"
                className="switch"
                checked={enabled}
                disabled={save.isPending || disabledOverridden}
                aria-describedby={
                  disabledOverridden
                    ? "router-router_disabled-override"
                    : undefined
                }
                onChange={(event) => edit({ enabled: event.target.checked })}
              />
              Enable public router
            </label>
          )}
          {disabledOverridden && (
            <span
              className="small muted router-override"
              id="router-router_disabled-override"
            >
              Set at startup by <code>STARTSHIP_ROUTER_DISABLED</code> or{" "}
              <code>--router-disabled</code>.
            </span>
          )}
          {!enabled && (
            <p className="note router-warning">
              When the router is disabled, application domains and HTTPS stop
              being served. The dashboard stays available on its own port.
            </p>
          )}
          {listenFields.map(([field, label, defaultPort]) => (
            <div key={field} className="router-listen">
              <ListenAddressPicker
                id={`router-${field}`}
                label={label}
                defaultPort={defaultPort}
                value={value(field)}
                interfaces={interfaces}
                readOnly={!admin}
                disabled={save.isPending}
                onChange={(next) => edit({ [field]: next })}
              />
            </div>
          ))}
          <dl className="kv">
            <dt>Config path</dt>
            <dd>
              <code>{cfg.config_path}</code>
            </dd>
          </dl>
          <ErrorMessage error={save.error} />
          {admin ? (
            <div className="form-actions">
              <button className="primary" disabled={!changed || save.isPending}>
                {save.isPending ? "Saving…" : "Save"}
              </button>
            </div>
          ) : (
            <p className="small muted">
              Only Platform Admins can change the router settings.
            </p>
          )}
        </form>
      )}
    </section>
  );
}
