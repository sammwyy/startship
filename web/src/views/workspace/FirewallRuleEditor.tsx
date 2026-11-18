import { useState } from "react";
import { Icon } from "../../components/Icon";
import { AsyncForm, Field } from "../../components/ui";
import { Picker, Toggle } from "./FirewallControls";
import { durationSeconds, durationText } from "../../services/firewall";
import type {
  Action,
  Catalog,
  Condition,
  IPList,
  Rule,
} from "../../services/firewall";

export function rulePreset(name: string): Rule {
  const rule: Rule = {
    id: "",
    name,
    enabled: true,
    match: "all",
    conditions: [{ field: "path", op: "prefix", value: "/" }],
    action: "block",
  };
  if (name === "Protect login endpoint")
    return {
      ...rule,
      conditions: [
        { field: "method", op: "eq", value: "POST" },
        { field: "path", op: "prefix", value: "/login" },
      ],
      action: "rate_limit",
      rate: { requests: 10, window_seconds: 60, by: "ip" },
      ban_seconds: 900,
    };
  if (name === "Block countries")
    return { ...rule, conditions: [{ field: "country", op: "in", value: [] }] };
  if (name === "Block AI crawlers")
    return {
      ...rule,
      match: "any",
      conditions: [
        {
          field: "user_agent",
          op: "matches",
          value:
            "(?i)(GPTBot|ClaudeBot|CCBot|Bytespider|ChatGPT-User|Google-Extended)",
        },
      ],
    };
  return rule;
}

function Chips({
  value,
  onChange,
  options,
}: {
  value: string[];
  onChange: (values: string[]) => void;
  options?: string[];
}) {
  const [text, setText] = useState("");
  function add() {
    const next = text
      .split(",")
      .map((v) => v.trim())
      .filter(Boolean);
    onChange([...new Set([...value, ...next])]);
    setText("");
  }
  return (
    <div className="fw-chip-field">
      <div className="fw-chips">
        {value.map((v) => (
          <button
            type="button"
            key={v}
            aria-label={`Remove ${v}`}
            onClick={() => onChange(value.filter((s) => s !== v))}
          >
            {v}
            <Icon name="close" />
          </button>
        ))}
      </div>
      {options ? (
        <Picker
          label="Add value"
          value=""
          options={options.filter((v) => !value.includes(v))}
          onChange={(v) => onChange([...value, v])}
        />
      ) : (
        <div className="fw-inline">
          <input
            aria-label="Condition values"
            value={text}
            placeholder="Values, comma-separated"
            onChange={(e) => setText(e.target.value)}
            onBlur={() => {
              if (text.trim()) add();
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === ",") {
                e.preventDefault();
                add();
              }
            }}
          />
          <button type="button" onClick={add}>
            Add
          </button>
        </div>
      )}
    </div>
  );
}

export function FirewallRuleEditor({
  initial,
  catalog,
  lists,
  onSave,
}: {
  initial: Rule;
  catalog: Catalog;
  lists: IPList[];
  onSave: (rule: Rule) => Promise<unknown>;
}) {
  const [rule, setRule] = useState<Rule>(structuredClone(initial));
  const [window, setWindow] = useState(
    durationText(initial.rate?.window_seconds || 60),
  );
  const [ban, setBan] = useState(durationText(initial.ban_seconds || 0));
  const setCondition = (i: number, patch: Partial<Condition>) =>
    setRule((r) => ({
      ...r,
      conditions: r.conditions.map((c, n) =>
        n === i ? { ...c, ...patch } : c,
      ),
    }));
  return (
    <AsyncForm
      className="fw-editor"
      onSubmit={async () => {
        const next = {
          ...rule,
          conditions: rule.conditions.map((c) => ({
            ...c,
            value: Array.isArray(c.value)
              ? c.value.map((v) => v.trim()).filter(Boolean)
              : c.value,
          })),
        };
        if (next.action === "rate_limit")
          next.rate = {
            requests: rule.rate?.requests || 10,
            window_seconds: durationSeconds(window),
            by: rule.rate?.by || "ip",
          };
        else delete next.rate;
        if (next.action === "ban" || next.action === "rate_limit")
          next.ban_seconds = durationSeconds(ban);
        else delete next.ban_seconds;
        await onSave(next);
      }}
    >
      <div className="modal-body">
        <Field
          label="Rule name"
          required
          maxLength={120}
          value={rule.name}
          onChange={(e) => setRule({ ...rule, name: e.target.value })}
        />
        <div className="fw-section-head">
          <div>
            <h3>When a request matches</h3>
            <p className="small muted">
              Conditions are evaluated in the order shown.
            </p>
          </div>
          <div className="fw-segments" aria-label="Condition match">
            {(["all", "any"] as const).map((v) => (
              <button
                type="button"
                key={v}
                aria-pressed={rule.match === v}
                onClick={() => setRule({ ...rule, match: v })}
              >
                {v === "all" ? "All conditions" : "Any condition"}
              </button>
            ))}
          </div>
        </div>
        <div className="fw-conditions">
          {rule.conditions.map((c, i) => {
            const field = catalog.fields.find(
              (f) =>
                f.id ===
                (c.field.startsWith("header.") ? "header.<name>" : c.field),
            );
            return (
              <div className="fw-condition" key={i}>
                <div className="fw-condition-selects">
                  <label>
                    Field
                    <Picker
                      label={`Condition ${i + 1} field`}
                      value={
                        c.field.startsWith("header.")
                          ? "header.<name>"
                          : c.field
                      }
                      options={catalog.fields.map((f) => f.id)}
                      onChange={(value) => {
                        const f = catalog.fields.find((f) => f.id === value)!;
                        setCondition(i, {
                          field:
                            value === "header.<name>"
                              ? "header.x-example"
                              : value,
                          op: f.ops[0],
                          value: f.values?.[0] || "",
                        });
                      }}
                    />
                  </label>
                  <label>
                    Operator
                    <Picker
                      label={`Condition ${i + 1} operator`}
                      value={c.op}
                      options={field?.ops || []}
                      onChange={(op) =>
                        setCondition(i, {
                          op,
                          value:
                            op === "in"
                              ? Array.isArray(c.value)
                                ? c.value
                                : c.value
                                  ? [c.value]
                                  : []
                              : Array.isArray(c.value)
                                ? c.value[0] || ""
                                : c.value,
                        })
                      }
                    />
                  </label>
                </div>
                {c.field.startsWith("header.") && (
                  <Field
                    label="Header name"
                    required
                    value={c.field.slice(7)}
                    onChange={(e) =>
                      setCondition(i, { field: `header.${e.target.value}` })
                    }
                  />
                )}
                {c.op !== "exists" && (
                  <div>
                    <span className="field-label">Value</span>
                    {c.op === "in" ? (
                      <Chips
                        value={Array.isArray(c.value) ? c.value : []}
                        options={field?.values}
                        onChange={(value) => setCondition(i, { value })}
                      />
                    ) : c.op === "in_list" ? (
                      <Picker
                        label={`Condition ${i + 1} list`}
                        value={String(c.value)}
                        options={lists.map((l) => ({
                          value: l.id,
                          label: l.name,
                        }))}
                        onChange={(value) => setCondition(i, { value })}
                      />
                    ) : field?.values ? (
                      <Picker
                        label={`Condition ${i + 1} value`}
                        value={String(c.value)}
                        options={field.values}
                        onChange={(value) => setCondition(i, { value })}
                      />
                    ) : (
                      <input
                        aria-label={`Condition ${i + 1} value`}
                        required
                        value={String(c.value)}
                        placeholder={
                          c.field === "country" ? "US" : "Match value"
                        }
                        onChange={(e) =>
                          setCondition(i, { value: e.target.value })
                        }
                      />
                    )}
                  </div>
                )}
                {c.field === "country" && !catalog.geoip && (
                  <p className="note fw-warning">
                    GeoIP database unavailable. Country matching needs
                    geoip_database or a trusted proxy country header.
                  </p>
                )}
                <div className="fw-condition-footer">
                  <label className="fw-check">
                    <input
                      type="checkbox"
                      checked={!!c.negate}
                      onChange={(e) =>
                        setCondition(i, { negate: e.target.checked })
                      }
                    />
                    Negate condition
                  </label>
                  <button
                    type="button"
                    className="icon-button danger"
                    disabled={rule.conditions.length === 1}
                    aria-label={`Remove condition ${i + 1}`}
                    onClick={() =>
                      setRule({
                        ...rule,
                        conditions: rule.conditions.filter((_, n) => n !== i),
                      })
                    }
                  >
                    <Icon name="trash" />
                  </button>
                </div>
              </div>
            );
          })}
        </div>
        <button
          type="button"
          disabled={rule.conditions.length >= 16}
          onClick={() =>
            setRule({
              ...rule,
              conditions: [
                ...rule.conditions,
                { field: "path", op: "prefix", value: "/" },
              ],
            })
          }
        >
          <Icon name="plus" />
          Add condition
        </button>
        <div className="fw-form-grid">
          <label>
            Then
            <Picker
              label="Rule action"
              value={rule.action}
              options={catalog.actions}
              onChange={(action) =>
                setRule({ ...rule, action: action as Action })
              }
            />
          </label>
          <div className="fw-enabled">
            <span>Enabled</span>
            <Toggle
              label="Rule enabled"
              checked={rule.enabled}
              onChange={() => setRule({ ...rule, enabled: !rule.enabled })}
            />
          </div>
        </div>
        {rule.action === "rate_limit" && (
          <div className="fw-form-grid">
            <Field
              label="Requests"
              type="number"
              min={1}
              required
              value={rule.rate?.requests ?? 10}
              onChange={(e) =>
                setRule({
                  ...rule,
                  rate: {
                    requests: Number(e.target.value),
                    window_seconds: 60,
                    by: rule.rate?.by || "ip",
                  },
                })
              }
            />
            <Field
              label="Window"
              value={window}
              onChange={(e) => setWindow(e.target.value)}
              required
            />
            <label>
              Count by
              <Picker
                label="Rate key"
                value={rule.rate?.by || "ip"}
                options={[
                  { value: "ip", label: "IP address" },
                  { value: "ip_path", label: "IP + path" },
                ]}
                onChange={(by) =>
                  setRule({
                    ...rule,
                    rate: {
                      requests: rule.rate?.requests || 10,
                      window_seconds: 60,
                      by: by as "ip" | "ip_path",
                    },
                  })
                }
              />
            </label>
          </div>
        )}
        {(rule.action === "ban" || rule.action === "rate_limit") && (
          <Field
            label="Ban duration (0 = permanent / no rate ban)"
            value={ban}
            onChange={(e) => setBan(e.target.value)}
            placeholder="15m"
          />
        )}
      </div>
      <div className="form-actions">
        <button type="submit" className="primary">
          Save rule
        </button>
      </div>
    </AsyncForm>
  );
}
