import type { ReactNode } from "react";
import { useUI } from "../providers/UIProvider";
import { Icon } from "./Icon";
export function Heading({
  title,
  kicker,
  subtitle,
  actions,
  project,
}: {
  title: string;
  kicker?: string;
  subtitle?: string;
  actions?: ReactNode;
  project?: string;
}) {
  const { openNavigation } = useUI();
  return (
    <header className="heading">
      <div className="heading-identity">
        <button
          className="icon-button mobile-only"
          aria-label="Open navigation"
          aria-haspopup="dialog"
          onClick={openNavigation}
        >
          <Icon name="menu" />
        </button>
        <div className="heading-copy">
          {kicker && <div className="eyebrow">{kicker}</div>}
          {project && <span className="mobile-project-name">{project}</span>}
          <h1>{title}</h1>
          {subtitle && <p aria-live="polite">{subtitle}</p>}
        </div>
      </div>
      {actions && <div className="row heading-actions">{actions}</div>}
    </header>
  );
}
