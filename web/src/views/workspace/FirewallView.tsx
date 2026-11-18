import { useEffect, useRef, useState } from "react";
import { NavLink, useParams } from "react-router-dom";
import { Heading } from "../../components/Heading";
import { Icon } from "../../components/Icon";
import { Empty, ErrorMessage, Loading } from "../../components/ui";
import { trafficNumber, trafficTime } from "../../components/TrafficCharts";
import { useDashboard } from "../../layouts/DashboardLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { useLiveStatus } from "../../providers/LiveProvider";
import {
  deleteRule,
  orderRules,
  savePolicy,
  saveRule,
  useFirewall,
  useFirewallCatalog,
} from "../../services/firewall";
import type { Action, Policy, Rule, Stats } from "../../services/firewall";
import { ActionPill, Picker, Toggle, optionLabel } from "./FirewallControls";
import { FirewallRuleEditor, rulePreset } from "./FirewallRuleEditor";
import { FirewallLimits } from "./FirewallLimits";
import { FirewallLists } from "./FirewallLists";
import "./FirewallView.css";

function FirewallChart({ stats }: { stats: Stats }) {
  const minutes = stats.minutes;
  const max = Math.max(2, ...minutes.map((m) => m.total));
  return (
    <section className="traffic-panel fw-chart">
      <div className="traffic-panel-head">
        <div>
          <h2>Request activity</h2>
          <span>Last 60 minutes · one-minute intervals</span>
        </div>
        <div className="traffic-legend">
          <span>
            <i />
            Allowed
          </span>
          <span>
            <i className="errors" />
            Blocked
          </span>
        </div>
      </div>
      <div className="traffic-volume-chart">
        <div className="traffic-y-axis">
          <span>{trafficNumber(max)}</span>
          <span>{trafficNumber(max / 2)}</span>
          <span>0</span>
        </div>
        <svg
          role="img"
          aria-label="Allowed and blocked requests per minute"
          viewBox={`0 0 ${Math.max(1, minutes.length) * 10 + 4} 164`}
          preserveAspectRatio="none"
        >
          <path className="traffic-gridline" d="M0 10H604M0 85H604M0 160H604" />
          {minutes.map((m, i) => {
            if (!m.total) return null;
            const total = (m.total / max) * 150,
              blocked = (m.blocked / max) * 150;
            return (
              <g key={m.at}>
                <title>
                  {trafficTime(m.at * 1000)} · {m.total - m.blocked} allowed ·{" "}
                  {m.blocked} blocked
                </title>
                <rect
                  className="traffic-bar"
                  x={4 + i * 10}
                  y={160 - total}
                  width={6}
                  height={total}
                  rx={1}
                />
                <rect
                  className="traffic-error-bar"
                  x={4 + i * 10}
                  y={160 - blocked}
                  width={6}
                  height={blocked}
                  rx={1}
                />
              </g>
            );
          })}
        </svg>
      </div>
      <div className="traffic-axis">
        <span>
          {minutes[0] ? trafficTime(minutes[0].at * 1000) : "60 min ago"}
        </span>
        <span>Now</span>
      </div>
    </section>
  );
}

export function FirewallView() {
  const { workspaceId = "", section = "overview" } = useParams();
  const { workspace } = useDashboard();
  const { can, user } = useSession();
  const ui = useUI();
  const allowed =
    !!user?.platform_admin ||
    (workspace?.id === workspaceId && can("firewall.view", workspace));
  const { data, liveError } = useFirewall(workspaceId, allowed);
  const catalog = useFirewallCatalog(allowed);
  const status = useLiveStatus();
  const [editor, setEditor] = useState<{ rule: Rule; creating: boolean }>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>();
  const dialog = useRef<HTMLDialogElement>(null);
  const manage = allowed && !liveError && !!data?.can_manage;
  useEffect(() => {
    if (editor && manage && !liveError) dialog.current?.showModal();
    else dialog.current?.close();
  }, [editor, manage, liveError]);
  async function change(policy: Policy) {
    setSaving(true);
    setError(undefined);
    try {
      await savePolicy(workspaceId, policy);
    } catch (e) {
      setError(e);
    } finally {
      setSaving(false);
    }
  }
  const rules = data?.policy.rules || [];
  const topRule = data?.stats.top_rules?.[0]?.value;
  const topRuleName =
    rules.find((rule) => rule.id === topRule)?.name ||
    catalog.data?.managed_rules.find((rule) => rule.id === topRule)?.name ||
    topRule ||
    "—";
  const closeEditor = () => setEditor(undefined);
  return (
    <div className="firewall-page traffic-page">
      <Heading
        kicker="Workspace security"
        title="Firewall"
        subtitle="Protect every project at the edge."
        actions={
          allowed &&
          data && (
            <div className="fw-segments fw-mode" aria-label="Firewall mode">
              {(["off", "monitor", "enforce"] as const).map((mode) => (
                <button
                  key={mode}
                  aria-pressed={data.policy.mode === mode}
                  disabled={!manage || saving}
                  onClick={() => void change({ ...data.policy, mode })}
                >
                  {mode[0].toUpperCase() + mode.slice(1)}
                </button>
              ))}
            </div>
          )
        }
      />
      {!allowed ? (
        <Empty title="Firewall access required">
          Ask a workspace administrator for firewall.view permission.
        </Empty>
      ) : liveError ? (
        <ErrorMessage error={liveError} />
      ) : !data ? (
        <>
          <ErrorMessage error={catalog.error} />
          <Loading />
        </>
      ) : (
        <>
          <ErrorMessage error={error} />
          <div className="fw-stats">
            {[
              ["Requests · 60m", trafficNumber(data.stats.total)],
              ["Blocked", trafficNumber(data.stats.blocked)],
              [
                "Block rate",
                `${(data.stats.total ? (data.stats.blocked / data.stats.total) * 100 : 0).toFixed(1)}%`,
              ],
              ["Top rule", topRuleName],
            ].map(([label, value]) => (
              <div key={label}>
                <span>{label}</span>
                <strong title={value}>{value}</strong>
              </div>
            ))}
          </div>
          <div className="fw-toolbar">
            <div className="fw-segments fw-tabs" aria-label="Firewall sections">
              {[
                ["overview", "Overview"],
                ["rules", "Rules"],
                ["limits", "Limits"],
                ["lists", "IP lists"],
              ].map(([key, label]) => (
                <NavLink
                  key={key}
                  to={`/workspace/${workspaceId}/firewall/${key}`}
                  className={section === key ? "active" : ""}
                >
                  {label}
                </NavLink>
              ))}
            </div>
            <span className="fw-meta">
              <i
                className={`fw-live ${status === "connected" ? "connected" : ""}`}
              />
              {status === "connected"
                ? "Live updates"
                : status === "disconnected"
                  ? "Disconnected"
                  : "Connecting…"}
              {!manage ? " · Read only" : ""}
            </span>
          </div>
          {data.policy.mode !== "enforce" && (
            <p className="note fw-mode-note">
              <Icon name="shield" />
              {data.policy.mode === "off"
                ? "Firewall is off. Requests are counted, but policies are not evaluated."
                : "Monitor mode evaluates and logs matches without blocking requests."}
            </p>
          )}
          {section === "overview" && (
            <div className="fw-stack">
              <FirewallChart stats={data.stats} />
              <section className="panel">
                <div className="fw-section-head fw-panel">
                  <div>
                    <h2>Recent events</h2>
                    <p>Latest 100 matches · newest first</p>
                  </div>
                  <span className="fw-meta">{data.events.length} events</span>
                </div>
                {data.events.length ? (
                  <div className="fw-table fw-events">
                    <table>
                      <thead>
                        <tr>
                          <th>Time</th>
                          <th>Client IP / country</th>
                          <th>Request</th>
                          <th>UA class</th>
                          <th>Rule</th>
                          <th>Action</th>
                        </tr>
                      </thead>
                      <tbody>
                        {data.events.map((event, i) => (
                          <tr key={`${event.time}-${i}`}>
                            <td data-label="Time">{trafficTime(event.time)}</td>
                            <td data-label="Client" className="fw-client">
                              <code>{event.ip}</code>
                              <span className="muted">
                                {" "}
                                · {event.country || "Unknown country"}
                              </span>
                            </td>
                            <td data-label="Request">
                              <span className="fw-request">
                                <b>{event.method}</b>
                                <span title={`${event.host}${event.path}`}>
                                  {event.host}
                                  {event.path}
                                </span>
                              </span>
                            </td>
                            <td data-label="UA class" title={event.ua}>
                              {optionLabel(event.ua_class || "unknown")}
                            </td>
                            <td data-label="Rule">
                              {event.rule_name || event.rule_id}
                            </td>
                            <td data-label="Action">
                              <ActionPill
                                action={event.action}
                                monitored={event.monitored}
                              />
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ) : (
                  <div className="fw-empty-state">
                    <Icon name="shield-check" />
                    <strong>No firewall events yet</strong>
                    <p>Matching requests will appear here in real time.</p>
                  </div>
                )}
              </section>
            </div>
          )}
          {section === "rules" && (
            <div className="fw-stack">
              <ErrorMessage error={catalog.error} />
              {catalog.data ? (
                <>
                  <section className="panel">
                    <div className="fw-section-head fw-panel">
                      <div>
                        <h2>Managed protection</h2>
                        <p>Built-in detection, maintained by Startship.</p>
                      </div>
                      <span className="fw-meta">
                        {catalog.data.managed_rules.length} detectors
                      </span>
                    </div>
                    <div className="fw-managed">
                      {catalog.data.managed_rules.map((item) => {
                        const managed = data.policy.managed[item.id] || {
                          enabled: false,
                          action: item.default_action,
                        };
                        return (
                          <div className="fw-managed-row" key={item.id}>
                            <Toggle
                              label={`Enable ${item.name}`}
                              checked={managed.enabled}
                              disabled={!manage || saving}
                              onChange={() =>
                                void change({
                                  ...data.policy,
                                  managed: {
                                    ...data.policy.managed,
                                    [item.id]: {
                                      ...managed,
                                      enabled: !managed.enabled,
                                    },
                                  },
                                })
                              }
                            />
                            <div className="fw-managed-copy">
                              <strong>{item.name}</strong>
                              <p>{item.description}</p>
                            </div>
                            <Picker
                              label={`${item.name} action`}
                              value={managed.action}
                              options={catalog.data!.actions.filter(
                                (a) => a !== "rate_limit",
                              )}
                              disabled={!manage || saving}
                              onChange={(action) =>
                                void change({
                                  ...data.policy,
                                  managed: {
                                    ...data.policy.managed,
                                    [item.id]: {
                                      ...managed,
                                      action: action as Action,
                                    },
                                  },
                                })
                              }
                            />
                          </div>
                        );
                      })}
                    </div>
                  </section>
                  <section className="panel">
                    <div className="fw-section-head fw-panel">
                      <div>
                        <h2>Custom rules</h2>
                        <p>
                          Evaluated in order. The first terminal match wins.
                        </p>
                      </div>
                      {manage && (
                        <button
                          className="primary"
                          onClick={() =>
                            setEditor({ rule: rulePreset(""), creating: true })
                          }
                        >
                          <Icon name="plus" />
                          Add rule
                        </button>
                      )}
                    </div>
                    {rules.length ? (
                      <div className="fw-custom-rules">
                        {rules.map((rule, i) => (
                          <div className="fw-custom-rule" key={rule.id}>
                            <span className="fw-order">{i + 1}</span>
                            <Toggle
                              label={`Enable ${rule.name}`}
                              checked={rule.enabled}
                              disabled={!manage}
                              onChange={() =>
                                ui.run(async () => {
                                  await saveRule(
                                    workspaceId,
                                    { ...rule, enabled: !rule.enabled },
                                    false,
                                  );
                                })
                              }
                            />
                            <div className="fw-managed-copy">
                              <strong>{rule.name}</strong>
                              <p>
                                {rule.match === "all" ? "All" : "Any"} of{" "}
                                {rule.conditions.length} conditions ·{" "}
                                {rule.conditions.map((c) => c.field).join(", ")}
                              </p>
                            </div>
                            <ActionPill action={rule.action} />
                            {manage && (
                              <div className="fw-rule-actions">
                                <button
                                  className="icon-button"
                                  aria-label={`Move ${rule.name} up`}
                                  disabled={i === 0}
                                  onClick={() =>
                                    ui.run(async () => {
                                      const ids = rules.map((r) => r.id);
                                      [ids[i - 1], ids[i]] = [
                                        ids[i],
                                        ids[i - 1],
                                      ];
                                      await orderRules(workspaceId, ids);
                                    })
                                  }
                                >
                                  ↑
                                </button>
                                <button
                                  className="icon-button"
                                  aria-label={`Move ${rule.name} down`}
                                  disabled={i === rules.length - 1}
                                  onClick={() =>
                                    ui.run(async () => {
                                      const ids = rules.map((r) => r.id);
                                      [ids[i], ids[i + 1]] = [
                                        ids[i + 1],
                                        ids[i],
                                      ];
                                      await orderRules(workspaceId, ids);
                                    })
                                  }
                                >
                                  ↓
                                </button>
                                <button
                                  onClick={() =>
                                    setEditor({ rule, creating: false })
                                  }
                                >
                                  Edit
                                </button>
                                <button
                                  className="icon-button danger"
                                  aria-label={`Delete ${rule.name}`}
                                  onClick={() =>
                                    ui.dialog({
                                      title: "Delete rule",
                                      description: `Delete ${rule.name}?`,
                                      submit: "Delete rule",
                                      onSubmit: async () => {
                                        await deleteRule(workspaceId, rule.id);
                                      },
                                    })
                                  }
                                >
                                  <Icon name="trash" />
                                </button>
                              </div>
                            )}
                          </div>
                        ))}
                      </div>
                    ) : (
                      <p className="fw-empty-row muted">
                        No custom rules. Start with a preset or build your own.
                      </p>
                    )}
                    {manage && (
                      <div className="fw-presets">
                        <span className="fw-meta">Start from a preset</span>
                        {[
                          "Protect login endpoint",
                          "Block countries",
                          "Block AI crawlers",
                        ].map((name) => (
                          <button
                            key={name}
                            onClick={() =>
                              setEditor({
                                rule: rulePreset(name),
                                creating: true,
                              })
                            }
                          >
                            {name}
                            <Icon name="plus" />
                          </button>
                        ))}
                      </div>
                    )}
                  </section>
                </>
              ) : (
                !catalog.error && <Loading />
              )}
            </div>
          )}
          {section === "limits" && (
            <FirewallLimits
              key={`${workspaceId}-${data.policy.updated}`}
              limits={data.policy.limits}
              canManage={manage}
              onSave={(limits) =>
                savePolicy(workspaceId, { ...data.policy, limits })
              }
            />
          )}
          {section === "lists" && (
            <FirewallLists
              workspaceId={workspaceId}
              lists={data.lists}
              canManage={manage}
            />
          )}
          {!["overview", "rules", "limits", "lists"].includes(section) && (
            <Empty title="Section not found">
              Choose Overview, Rules, Limits or IP lists.
            </Empty>
          )}
        </>
      )}
      <dialog
        ref={dialog}
        className="fw-dialog"
        aria-labelledby="fw-editor-title"
        onClose={closeEditor}
        onCancel={closeEditor}
      >
        {editor && catalog.data && manage && (
          <>
            <div className="row">
              <div>
                <span className="eyebrow">Custom protection</span>
                <h2 id="fw-editor-title">
                  {editor.creating ? "Create rule" : "Edit rule"}
                </h2>
              </div>
              <button
                className="icon-button"
                aria-label="Close rule editor"
                onClick={closeEditor}
              >
                <Icon name="close" />
              </button>
            </div>
            <FirewallRuleEditor
              key={editor.rule.id + editor.rule.name}
              initial={editor.rule}
              catalog={catalog.data}
              lists={data?.lists || []}
              onSave={async (rule) => {
                await saveRule(workspaceId, rule, editor.creating);
                closeEditor();
                ui.notify("Rule saved");
              }}
            />
          </>
        )}
      </dialog>
    </div>
  );
}
