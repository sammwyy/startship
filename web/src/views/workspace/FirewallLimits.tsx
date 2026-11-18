import { useState } from "react";
import { AsyncForm, Field } from "../../components/ui";
import { Picker } from "./FirewallControls";
import { durationSeconds, durationText } from "../../services/firewall";
import type { Limits } from "../../services/firewall";

function Size({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
}) {
  const units = { B: 1, KB: 1000, MB: 1000000 };
  const [unit, setUnit] = useState<keyof typeof units>(
    value && value % 1000000 === 0
      ? "MB"
      : value && value % 1000 === 0
        ? "KB"
        : "B",
  );
  return (
    <label>
      {label}
      <div className="fw-size">
        <input
          aria-label={label}
          type="number"
          min={0}
          step="any"
          value={value / units[unit]}
          onChange={(e) => onChange(Number(e.target.value) * units[unit])}
        />
        <Picker
          label={`${label} unit`}
          value={unit}
          options={Object.keys(units)}
          onChange={(u) => {
            const next = u as keyof typeof units;
            onChange((value / units[unit]) * units[next]);
            setUnit(next);
          }}
        />
      </div>
    </label>
  );
}
export function FirewallLimits({
  limits,
  canManage,
  onSave,
}: {
  limits: Limits;
  canManage: boolean;
  onSave: (limits: Limits) => Promise<unknown>;
}) {
  const [draft, setDraft] = useState(structuredClone(limits));
  const [requests, setRequests] = useState(limits.rate?.requests || 0);
  const [window, setWindow] = useState(
    durationText(limits.rate?.window_seconds || 60),
  );
  const [action, setAction] = useState<"block" | "ban">(
    limits.rate?.action || "block",
  );
  const [ban, setBan] = useState(durationText(limits.rate?.ban_seconds || 900));
  return (
    <AsyncForm
      className="fw-stack"
      disabled={!canManage}
      onSubmit={async () => {
        if (
          [
            draft.max_body_bytes,
            draft.max_url_bytes,
            draft.max_header_bytes,
          ].some((n) => !Number.isSafeInteger(n))
        )
          throw new Error("Sizes must be whole bytes.");
        await onSave({
          ...draft,
          rate: requests
            ? {
                requests,
                window_seconds: durationSeconds(window),
                action,
                ban_seconds: action === "ban" ? durationSeconds(ban) : 0,
              }
            : null,
        });
      }}
    >
      <section className="panel fw-panel">
        <div className="fw-section-head">
          <div>
            <h2>Request rate</h2>
            <p>Per client IP, across every route in this workspace.</p>
          </div>
          <span className="fw-meta">0 requests = disabled</span>
        </div>
        <div className="fw-form-grid">
          <Field
            label="Requests"
            type="number"
            min={0}
            required
            value={requests}
            onChange={(e) => setRequests(Number(e.target.value))}
          />
          <Field
            label="Per window"
            required
            value={window}
            onChange={(e) => setWindow(e.target.value)}
          />
          <label>
            When exceeded
            <Picker
              label="Rate action"
              value={action}
              options={["block", "ban"]}
              onChange={(v) => setAction(v as "block" | "ban")}
            />
          </label>
          {action === "ban" && (
            <Field
              label="Ban duration"
              value={ban}
              required
              onChange={(e) => setBan(e.target.value)}
            />
          )}
        </div>
      </section>
      <section className="panel fw-panel">
        <div className="fw-section-head">
          <div>
            <h2>Request limits</h2>
            <p>Bound resource usage before requests reach your applications.</p>
          </div>
          <span className="fw-meta">0 = unlimited</span>
        </div>
        <div className="fw-form-grid">
          <Field
            label="Concurrent requests per IP"
            type="number"
            min={0}
            required
            value={draft.concurrent_per_ip}
            onChange={(e) =>
              setDraft({ ...draft, concurrent_per_ip: Number(e.target.value) })
            }
          />
          <Size
            label="Max body"
            value={draft.max_body_bytes}
            onChange={(v) => setDraft({ ...draft, max_body_bytes: v })}
          />
          <Size
            label="Max URL"
            value={draft.max_url_bytes}
            onChange={(v) => setDraft({ ...draft, max_url_bytes: v })}
          />
          <Size
            label="Max headers"
            value={draft.max_header_bytes}
            onChange={(v) => setDraft({ ...draft, max_header_bytes: v })}
          />
        </div>
      </section>
      <div className="form-actions">
        <span className="small muted">
          Changes apply to every project in this workspace.
        </span>
        {canManage && (
          <button type="submit" className="primary">
            Save limits
          </button>
        )}
      </div>
    </AsyncForm>
  );
}
