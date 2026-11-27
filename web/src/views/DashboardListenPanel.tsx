import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ListenAddressPicker } from "../components/ListenAddressPicker";
import { ErrorMessage, Loading } from "../components/ui";
import { useLiveSubscription } from "../providers/LiveProvider";
import { useSession } from "../providers/SessionProvider";
import { request } from "../services/api";
import { queries } from "../services/queries";
import type { DashboardConfig, DashboardSettings } from "../services/types";

export function DashboardListenPanel() {
  const { user } = useSession();
  const admin = !!user?.platform_admin;
  const client = useQueryClient();
  const query = queries.dashboard();
  const status = useQuery(query);
  const interfacesQuery = useQuery(queries.interfaces());
  useLiveSubscription({ topic: "system_dashboard" });
  const [draft, setDraft] = useState<string>();
  const save = useMutation({
    mutationFn: (body: DashboardSettings) =>
      request<DashboardConfig>("system/dashboard", body, "PUT"),
    onSuccess: (data) => {
      void client.cancelQueries({ queryKey: query.queryKey, exact: true });
      client.setQueryData(query.queryKey, data);
      setDraft(undefined);
    },
  });
  const cfg = status.data;
  const overridden = cfg?.overridden.includes("dashboard_listen");
  const editable = admin && !overridden;
  const value =
    (editable ? draft : undefined) ?? cfg?.dashboard_listen.join(", ") ?? "";
  const addresses = value
    .split(",")
    .map((entry) => entry.trim())
    .filter(Boolean);
  const changed =
    !!cfg && addresses.join(",") !== cfg.dashboard_listen.join(",");
  const interfaces = interfacesQuery.data?.interfaces ?? [];

  return (
    <section
      className="panel panel-pad"
      aria-labelledby="dashboard-listen-title"
    >
      <div className="section-head">
        <div>
          <h2 id="dashboard-listen-title">Dashboard</h2>
          <p className="small muted">
            Admin panel and API listen interface. Choose Any interface or bind
            to a specific interface such as a VPN.
          </p>
        </div>
      </div>
      {status.isPending && <Loading />}
      <ErrorMessage error={status.error} />
      {cfg && (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (admin && changed && !save.isPending) {
              save.mutate({ dashboard_listen: addresses });
            }
          }}
        >
          <div className="router-listen">
            <ListenAddressPicker
              id="dashboard-listen"
              label="Dashboard listen addresses"
              defaultPort="8080"
              value={value}
              interfaces={interfaces}
              readOnly={!admin}
              disabled={save.isPending || overridden}
              describedBy={overridden ? "dashboard-listen-override" : undefined}
              onChange={(next) => {
                setDraft(next);
                if (save.isError) save.reset();
              }}
            />
            {overridden && (
              <span
                className="small muted router-override"
                id="dashboard-listen-override"
              >
                Set at startup by <code>STARTSHIP_DASHBOARD_ADDR</code> or{" "}
                <code>--dashboard-listen</code>.
              </span>
            )}
          </div>
          {admin && changed && (
            <p className="note router-warning">
              Saving closes the previous dashboard listener. Make sure the new
              interface and port are reachable from your browser.
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
              Only Platform Admins can change the dashboard settings.
            </p>
          )}
        </form>
      )}
    </section>
  );
}
