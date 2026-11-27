import type { ReactNode } from "react";
import "./Skeleton.css";

export function Skeleton({
  width = "100%",
  height = "0.75rem",
}: {
  width?: string;
  height?: string;
}) {
  return (
    <span aria-hidden="true" className="skeleton" style={{ width, height }} />
  );
}

export type LoadingLayout =
  "rows" | "table" | "cards" | "stats" | "workspace" | "project" | "page";

const slots = [0, 1, 2];

function Cards() {
  return (
    <div className="grid">
      {slots.map((slot) => (
        <div className="card skeleton-card" key={slot}>
          <Skeleton width="2rem" height="2rem" />
          <Skeleton width="58%" height="1rem" />
          <Skeleton width="77%" />
          <Skeleton width="44%" />
        </div>
      ))}
    </div>
  );
}

function Rows({ table = false }: { table?: boolean }) {
  return (
    <div className={table ? "panel skeleton-table" : "skeleton-rows"}>
      {table && (
        <div className="skeleton-row skeleton-table-head">
          <Skeleton width="35%" />
          <Skeleton width="44%" />
          <Skeleton width="48%" />
        </div>
      )}
      {slots.map((slot) => (
        <div className="skeleton-row" key={slot}>
          <Skeleton width={["66%", "52%", "74%"][slot]} />
          {table && <Skeleton width="56%" />}
          <Skeleton width="55%" />
        </div>
      ))}
    </div>
  );
}

function Stats() {
  return (
    <div className="stats">
      {slots.map((slot) => (
        <div className="stat skeleton-stat" key={slot}>
          <Skeleton width="min(85px, 100%)" />
          <Skeleton width="2.25rem" height="1.5rem" />
        </div>
      ))}
    </div>
  );
}

function Page({ children }: { children: ReactNode }) {
  return (
    <>
      <div className="skeleton-heading">
        <Skeleton width="6rem" />
        <Skeleton width="min(14rem, 100%)" height="1.75rem" />
        <Skeleton width="min(21rem, 100%)" />
      </div>
      {children}
    </>
  );
}

export function Loading({
  layout = "rows",
  label = "Loading…",
}: {
  layout?: LoadingLayout;
  label?: string;
}) {
  return (
    <div role="status" aria-label={label} className="skeleton-region">
      <span className="skeleton-status">{label}</span>
      <div aria-hidden="true">
        {layout === "rows" && <Rows />}
        {layout === "table" && <Rows table />}
        {layout === "cards" && <Cards />}
        {layout === "stats" && (
          <>
            <Stats />
            <Rows table />
          </>
        )}
        {layout === "workspace" && (
          <Page>
            <Stats />
            <Cards />
          </Page>
        )}
        {layout === "project" && (
          <Page>
            <div className="project-overview">
              <Rows table />
              <Rows table />
            </div>
          </Page>
        )}
        {layout === "page" && (
          <Page>
            <Rows table />
          </Page>
        )}
      </div>
    </div>
  );
}
