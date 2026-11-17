import { useEffect, useState } from "react";
import type { FormEvent, InputHTMLAttributes, ReactNode } from "react";
import { Icon } from "./Icon";

export function Field({
  label,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & { label: string }) {
  return (
    <label>
      {label}
      <input {...props} />
    </label>
  );
}
export function Badge({ status }: { status?: string }) {
  return (
    <span className={`badge ${status || "idle"}`}>
      {status || "Not deployed"}
    </span>
  );
}
export function Empty({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <div className="panel empty">
      <span className="card-icon">
        <Icon name="box" />
      </span>
      <strong>{title}</strong>
      {children}
    </div>
  );
}
export function Loading() {
  return (
    <p className="panel-pad" role="status">
      Loading…
    </p>
  );
}
export function ErrorMessage({ error }: { error: unknown }) {
  return error ? (
    <div className="form-error" role="alert">
      {error instanceof Error ? error.message : String(error)}
    </div>
  ) : null;
}
export function AsyncForm({
  onSubmit,
  children,
  className,
  disabled = false,
}: {
  onSubmit: (data: FormData, form: HTMLFormElement) => Promise<unknown>;
  children: ReactNode;
  className?: string;
  disabled?: boolean;
}) {
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>();
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving || disabled) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    setSaving(true);
    setError(undefined);
    try {
      await onSubmit(data, form);
    } catch (err) {
      setError(err);
    } finally {
      setSaving(false);
    }
  }
  return (
    <form className={className} onSubmit={submit}>
      <fieldset className="form-fields" disabled={disabled || saving}>
        {children}
      </fieldset>
      <ErrorMessage error={error} />
    </form>
  );
}
export function ThemeButton() {
  const [theme, setTheme] = useState(
    document.documentElement.dataset.theme || "dark",
  );
  const next = theme === "light" ? "dark" : "light";
  useEffect(() => {
    const sync = () =>
      setTheme(document.documentElement.dataset.theme || "dark");
    window.addEventListener("theme-changed", sync);
    return () => window.removeEventListener("theme-changed", sync);
  }, []);
  return (
    <button
      type="button"
      className="theme-toggle"
      aria-label={`Switch to ${next} theme`}
      onClick={() => {
        document.documentElement.dataset.theme = next;
        setTheme(next);
        window.dispatchEvent(new Event("theme-changed"));
        try {
          localStorage.setItem("startship-theme", next);
        } catch {
          /* Theme remains usable without storage. */
        }
      }}
    >
      <Icon name={next === "light" ? "sun" : "moon"} />
      <span>{next === "light" ? "Light" : "Dark"}</span>
    </button>
  );
}
