import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ErrorMessage, Loading } from "../components/ui";
import { useLiveSubscription } from "../providers/LiveProvider";
import { useSession } from "../providers/SessionProvider";
import { request } from "../services/api";
import { queries } from "../services/queries";
import type {
  RouterConfig,
  RouterOverride,
  RouterSettings,
} from "../services/types";

const overrides: Record<RouterOverride, [string, string]> = {
  router_disabled: ["STARTSHIP_ROUTER_DISABLED", "--router-disabled"],
  http_listen: ["STARTSHIP_HTTP_ADDR", "--http-listen"],
  https_listen: ["STARTSHIP_HTTPS_ADDR", "--https-listen"],
};
const listenFields = [
  ["http_listen", "HTTP listen addresses"],
  ["https_listen", "HTTPS listen addresses"],
] as const;

function OverrideNote({ field }: { field: RouterOverride }) {
  const [env, flag] = overrides[field];
  return (
    <span
      className="small muted router-override"
      id={`router-${field}-override`}
    >
      Set at startup by <code>{env}</code> or <code>{flag}</code>.
    </span>
  );
}

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
  const overridden = (field: RouterOverride) => cfg?.overridden.includes(field);
  const editable = (field: RouterOverride) => admin && !overridden(field);
  const enabled =
    (editable("router_disabled") ? draft.enabled : undefined) ??
    cfg?.enabled ??
    false;
  const value = (field: (typeof listenFields)[number][0]) =>
    (editable(field) ? draft[field] : undefined) ??
    cfg?.[field].join(", ") ??
    "";
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
                disabled={save.isPending || overridden("router_disabled")}
                aria-describedby={
                  overridden("router_disabled")
                    ? "router-router_disabled-override"
                    : undefined
                }
                onChange={(event) => edit({ enabled: event.target.checked })}
              />
              Enable public router
            </label>
          )}
          {overridden("router_disabled") && (
            <OverrideNote field="router_disabled" />
          )}
          {!enabled && (
            <p className="note router-warning">
              When the router is disabled, application domains and HTTPS stop
              being served. The dashboard stays available on its own port.
            </p>
          )}
          {listenFields.map(([field, label]) => (
            <div key={field} className="router-listen">
              <label htmlFor={`router-${field}`}>
                {label}
                <input
                  id={`router-${field}`}
                  value={value(field)}
                  readOnly={!admin}
                  disabled={save.isPending || overridden(field)}
                  aria-describedby={
                    overridden(field) ? `router-${field}-override` : undefined
                  }
                  spellCheck={false}
                  onChange={(event) => edit({ [field]: event.target.value })}
                />
              </label>
              {overridden(field) && <OverrideNote field={field} />}
            </div>
          ))}
          {admin && (
            <p className="small muted">
              Separate multiple listen addresses with commas.
            </p>
          )}
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
