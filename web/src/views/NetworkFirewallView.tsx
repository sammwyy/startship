import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useSession } from "../providers/SessionProvider";
import { useUI } from "../providers/UIProvider";
import { Icon } from "../components/Icon";
import { Empty, ErrorMessage, Field, Loading } from "../components/ui";
import {
  addNetworkRule,
  deleteNetworkRule,
  durationSeconds,
  networkPath,
  useNetworkFirewall,
} from "../services/firewall";
import type { NetworkRule } from "../services/firewall";
import { Expiry, Picker, optionLabel } from "./workspace/FirewallControls";
import "./workspace/FirewallView.css";

function NetworkFields() {
  const [kind, setKind] = useState<NetworkRule["kind"]>("ip");
  const [scope, setScope] = useState("all");
  return (
    <div className="fw-stack">
      <label>
        Kind
        <Picker
          label="Network rule kind"
          value={kind}
          options={["ip", "cidr", "country"]}
          onChange={(v) => setKind(v as NetworkRule["kind"])}
        />
        <input type="hidden" name="kind" value={kind} />
      </label>
      <Field
        name="value"
        label="Value"
        required
        placeholder={
          kind === "ip"
            ? "192.0.2.1"
            : kind === "cidr"
              ? "203.0.113.0/24"
              : "US"
        }
      />
      <label>
        Scope
        <Picker
          label="Network rule scope"
          value={scope}
          options={[
            { value: "all", label: "All ports and protocols" },
            { value: "router", label: "Router HTTP / HTTPS ports only" },
          ]}
          onChange={setScope}
        />
        <input type="hidden" name="ports" value={scope} />
      </label>
      <Field
        name="duration"
        label="Expires after (optional)"
        placeholder="1h — blank is permanent"
      />
      <Field name="note" label="Note" maxLength={4096} />
      <p className="note">
        All-port rules can block dashboard access. Keep a local Unix socket
        session available. Country rules require geoip_database.
      </p>
    </div>
  );
}
export function NetworkFirewallView() {
  const { can } = useSession();
  const allowed = can("network.firewall");
  const { data, error, isPending } = useNetworkFirewall(allowed);
  const ui = useUI();
  const client = useQueryClient();
  const refresh = () => client.invalidateQueries({ queryKey: [networkPath] });
  return (
    <div className="firewall-page fw-network">
      {!allowed ? (
        <Empty title="Network firewall access required">
          This page requires the platform network.firewall permission.
        </Empty>
      ) : (
        <>
          <ErrorMessage error={error} />
          {isPending ? (
            <Loading />
          ) : (
            data && (
              <>
                <section className="panel fw-panel">
                  <div className="fw-section-head">
                    <div className="fw-network-status">
                      <span
                        className={`fw-status-icon ${data.enabled && data.available && !data.error ? "ready" : ""}`}
                      >
                        <Icon name="shield" />
                      </span>
                      <div>
                        <h2>
                          {data.enabled
                            ? "Network protection"
                            : "Network firewall is disabled"}
                        </h2>
                        <p>
                          {data.enabled
                            ? "Kernel-level blocking, before requests reach Startship."
                            : "Opt in on the host to manage packet-filtering rules."}
                        </p>
                      </div>
                    </div>
                    <div className="fw-status-badges">
                      <span
                        className={`fw-action ${data.enabled ? "fw-action-allow" : ""}`}
                      >
                        {data.enabled ? "Enabled" : "Disabled"}
                      </span>
                      <span
                        className={`fw-action ${data.available ? "fw-action-allow" : "fw-action-block"}`}
                      >
                        {data.available ? "Available" : "Unavailable"}
                      </span>
                    </div>
                  </div>
                  {data.enabled && (
                    <ErrorMessage
                      error={
                        /operation not permitted|permission denied/i.test(
                          data.error || "",
                        )
                          ? "Missing CAP_NET_ADMIN — see docs/firewall.md for required host capabilities."
                          : data.error
                      }
                    />
                  )}
                  {!data.enabled && (
                    <div className="fw-network-enable">
                      <h3>Enable on your daemon</h3>
                      <p>
                        Add this to <code>/etc/startship/startship.toml</code>,
                        then restart with the required network capabilities:
                      </p>
                      <pre>
                        <code>network_firewall = true</code>
                      </pre>
                      <p>
                        Or install with{" "}
                        <code>STARTSHIP_NETWORK_FIREWALL=1</code>:
                      </p>
                      <pre>
                        <code>
                          sudo STARTSHIP_NETWORK_FIREWALL=1 sh install.sh
                        </code>
                      </pre>
                      <p className="small muted">
                        Requires ipset, iptables and ip6tables, plus
                        CAP_NET_ADMIN and CAP_NET_RAW. The installer configures
                        both capability sets. Environment values override TOML.
                      </p>
                    </div>
                  )}
                </section>
                <section className="panel">
                  <div className="fw-section-head fw-panel">
                    <div>
                      <h2>Network rules</h2>
                      <p>
                        IP addresses, networks and countries blocked on this
                        host.
                      </p>
                    </div>
                    <button
                      className="primary"
                      disabled={!data.enabled || !data.available}
                      onClick={() =>
                        ui.dialog({
                          title: "Add network rule",
                          submit: "Block traffic",
                          fields: <NetworkFields />,
                          onSubmit: async (f) => {
                            await addNetworkRule({
                              kind: String(
                                f.get("kind"),
                              ) as NetworkRule["kind"],
                              value: String(f.get("value")),
                              ports: String(f.get("ports")) as "all" | "router",
                              ttl_seconds: durationSeconds(
                                String(f.get("duration")),
                              ),
                              note: String(f.get("note")),
                            });
                            await refresh();
                            ui.notify("Network rule added");
                          },
                        })
                      }
                    >
                      <Icon name="plus" />
                      Add rule
                    </button>
                  </div>
                  {data.rules.length ? (
                    <div className="fw-table">
                      <table>
                        <thead>
                          <tr>
                            <th>Value</th>
                            <th>Kind</th>
                            <th>Scope</th>
                            <th>Expires</th>
                            <th>Note</th>
                            <th>
                              <span className="sr-only">Actions</span>
                            </th>
                          </tr>
                        </thead>
                        <tbody>
                          {data.rules.map((rule) => (
                            <tr key={rule.id}>
                              <td data-label="Value">
                                <code>{rule.value}</code>
                              </td>
                              <td data-label="Kind">
                                {optionLabel(rule.kind)}
                              </td>
                              <td data-label="Scope">
                                {rule.ports === "router"
                                  ? "Router only"
                                  : "All ports"}
                              </td>
                              <td data-label="Expires">
                                <Expiry value={rule.expires_at} />
                              </td>
                              <td data-label="Note" className="fw-wrap">
                                {rule.note || "—"}
                              </td>
                              <td data-label="Actions">
                                <button
                                  className="icon-button danger"
                                  aria-label={`Remove ${rule.value}`}
                                  onClick={() =>
                                    ui.dialog({
                                      title: "Remove network rule",
                                      description: `Stop blocking ${rule.value}?`,
                                      submit: "Remove rule",
                                      onSubmit: async () => {
                                        await deleteNetworkRule(rule.id);
                                        await refresh();
                                        ui.notify("Network rule removed");
                                      },
                                    })
                                  }
                                >
                                  <Icon name="trash" />
                                </button>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  ) : (
                    <div className="fw-empty-state">
                      <Icon name="shield-check" />
                      <strong>No network rules</strong>
                      <p>
                        {data.enabled
                          ? "Add an IP address, CIDR or country to block traffic."
                          : "Enable the network firewall to add rules."}
                      </p>
                    </div>
                  )}
                </section>
                <p className="small muted fw-footnote">
                  Network rules affect the host, not a workspace. They see the
                  direct peer’s IP, not forwarded HTTP headers.
                </p>
              </>
            )
          )}
        </>
      )}
    </div>
  );
}
