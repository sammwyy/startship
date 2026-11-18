import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { Icon } from "./Icon";
import { Popover } from "./Popover";
import { useUI } from "../providers/UIProvider";
import { orderedConnectionEnv, secretValue } from "../services/services";
import "./services.css";

export function ServiceModal({
  title,
  eyebrow = "WORKSPACE INFRASTRUCTURE",
  description,
  children,
  onClose,
  className = "",
}: {
  title: string;
  eyebrow?: string;
  description?: string;
  children: ReactNode;
  onClose: () => void;
  className?: string;
}) {
  const ref = useRef<HTMLDialogElement>(null),
    id = useId();
  useEffect(() => {
    ref.current?.showModal();
  }, []);
  return (
    <dialog
      ref={ref}
      className={`service-modal ${className}`}
      aria-labelledby={id}
      onCancel={onClose}
      onClose={onClose}
    >
      <div className="service-modal-head">
        <div>
          <span className="eyebrow">{eyebrow}</span>
          <h2 id={id}>{title}</h2>
          {description && <p>{description}</p>}
        </div>
        <button
          type="button"
          className="icon-button"
          aria-label="Close dialog"
          onClick={onClose}
        >
          <Icon name="close" />
        </button>
      </div>
      {children}
    </dialog>
  );
}
export function ServicePicker({
  label,
  value,
  options,
  onChange,
  disabled = false,
}: {
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const id = useId();
  return (
    <div className="service-picker">
      <span className="service-field-label">{label}</span>
      <Popover
        id={id}
        label={label}
        disabled={disabled}
        className="service-picker-trigger"
        menuRole="listbox"
        trigger={
          <>
            <span>
              {options.find((o) => o.value === value)?.label ||
                "Choose an option"}
            </span>
            <Icon name="chevron" />
          </>
        }
      >
        {options.map((o) => (
          <button
            type="button"
            role="option"
            aria-selected={o.value === value}
            key={o.value}
            onClick={() => onChange(o.value)}
          >
            <span>{o.label}</span>
            {o.value === value && <Icon name="check" />}
          </button>
        ))}
      </Popover>
    </div>
  );
}
export function CopyValue({ value, label }: { value: string; label?: string }) {
  const ui = useUI();
  return (
    <span className="copy-value">
      <code title={label || value}>{label || value}</code>
      <button
        type="button"
        className="icon-button"
        aria-label={`Copy ${label || value}`}
        onClick={() =>
          ui.run(async () => {
            await navigator.clipboard.writeText(value);
            ui.notify("Copied to clipboard");
          })
        }
      >
        <Icon name="copy" />
      </button>
    </span>
  );
}
export function EnvPreview({
  env,
  canReveal = false,
}: {
  env: Record<string, string>;
  canReveal?: boolean;
}) {
  const [reveal, setReveal] = useState(false);
  return (
    <div className="env-preview">
      <div className="service-section-label">
        <span>INJECTED ENVIRONMENT</span>
        {canReveal && (
          <button type="button" onClick={() => setReveal(!reveal)}>
            <Icon name="eye" />
            {reveal ? "Hide secrets" : "Show secrets"}
          </button>
        )}
      </div>
      {orderedConnectionEnv(env).map(([key, value]) => (
        <div className="env-line" key={key}>
          <span>{key}</span>
          {secretValue(key) && !reveal ? (
            <span className="masked-secret">••••••••</span>
          ) : (
            <code>{value}</code>
          )}
          <CopyValue value={value} label={key} />
        </div>
      ))}
    </div>
  );
}
export function RedeployNote() {
  return (
    <div className="service-notice" role="status">
      <Icon name="rocket" />
      <span>
        <strong>Redeploy to apply</strong> · Your connection and volume changes
        will be included in the next deployment.
      </span>
    </div>
  );
}
