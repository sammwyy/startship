import { useState } from "react";
import type {
  Container,
  DomainRoute,
  WorkspaceDomain,
} from "../services/types";
import { domainPatterns, HostPicker, pickedHost } from "./HostPicker";
import { AsyncForm, Field } from "./ui";
import { Icon } from "./Icon";

export interface RouteInput {
  host: string;
  container_id: string;
  path_prefix: string;
  port: number;
  strip_prefix: boolean;
}

export function RouteEditor({
  containers,
  domains,
  route,
  admin,
  onSave,
  onCancel,
}: {
  containers: Container[];
  domains: WorkspaceDomain[];
  route?: DomainRoute;
  admin?: boolean;
  onSave: (data: RouteInput) => Promise<void>;
  onCancel: () => void;
}) {
  const previous = route?.container_id || route?.app_id;
  const previousAvailable = containers.some((c) => c.id === previous);
  const initial = previousAvailable ? previous! : containers[0]?.id || "";
  const [containerId, setContainerId] = useState(initial);
  const target = containers.find((c) => c.id === containerId);
  const detectedPort = target?.port || 8080;
  const [port, setPort] = useState(String(route?.port || detectedPort));
  const [path, setPath] = useState(route?.path_prefix || "/");
  const [host, setHost] = useState(
    route?.host ||
      domains.flatMap(domainPatterns)[0]?.replace("*.", "[subdomain].") ||
      "",
  );
  const [strip, setStrip] = useState(route?.strip_prefix || false);
  const available = !!target && (!!domains.length || !!admin || !!route?.host);
  const previewPath = path.startsWith("/") ? path : `/${path}`;
  return (
    <section
      className="panel route-editor"
      aria-labelledby="route-editor-title"
    >
      <div className="route-editor-heading">
        <div>
          <h3 id="route-editor-title">
            {route ? "Edit route" : "Connect a domain"}
          </h3>
          <p className="small muted">
            Choose the public address, then where it should go.
          </p>
        </div>
        <button
          type="button"
          className="icon-button"
          aria-label="Close route editor"
          onClick={onCancel}
        >
          <Icon name="close" />
        </button>
      </div>
      <AsyncForm
        onSubmit={async (form) => {
          if (!available)
            throw new Error(
              "Choose an available container and workspace domain.",
            );
          await onSave({
            host: pickedHost(form),
            container_id: containerId,
            path_prefix: path.startsWith("/") ? path : `/${path}`,
            port: Number(port) === detectedPort ? 0 : Number(port),
            strip_prefix: strip,
          });
        }}
      >
        {previous && !previousAvailable && (
          <p className="note">
            The previous container is unavailable. Choose the new destination
            before saving.
          </p>
        )}
        <div className="route-editor-grid">
          <div className="route-editor-section">
            <div className="route-step">
              <Icon name="globe" />
              <strong>Public address</strong>
            </div>
            <HostPicker
              domains={domains}
              host={route?.host}
              admin={admin}
              onHostChange={setHost}
            />
            <Field
              label="Path"
              name="path_prefix"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              placeholder="/"
              required
              autoFocus
            />
            <p className="small muted">
              Use / for the whole site, or /api for a specific path.
            </p>
          </div>
          <div className="route-editor-section">
            <div className="route-step">
              <Icon name="box" />
              <strong>Forward to</strong>
            </div>
            {containers.length > 1 ? (
              <label>
                Container
                <select
                  name="container_id"
                  value={containerId}
                  onChange={(e) => {
                    setContainerId(e.target.value);
                    setPort(
                      String(
                        containers.find((c) => c.id === e.target.value)?.port ||
                          8080,
                      ),
                    );
                  }}
                >
                  {!target && (
                    <option value={containerId} disabled>
                      Container unavailable — choose another
                    </option>
                  )}
                  {containers.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
              </label>
            ) : (
              <div className="route-auto-target">
                <span className="field-label">Container</span>
                <strong>{target?.name || "Container unavailable"}</strong>
                <span className="small muted">
                  {target
                    ? "Selected automatically · only container"
                    : "Close this editor and reconnect the route to an available container."}
                </span>
              </div>
            )}
            <Field
              label="Container port"
              name="port"
              type="number"
              min={1}
              max={65535}
              required
              value={port}
              onChange={(e) => setPort(e.target.value)}
            />
            <p className="small muted">
              Detected port: {detectedPort}. Override it for this route only.
            </p>
          </div>
        </div>
        <div className="route-preview" aria-live="polite">
          <span>
            <Icon name="globe" />
            <strong>
              {host || "your-domain.com"}
              {previewPath}
            </strong>
          </span>
          <Icon name="next" />
          <span>
            <Icon name="box" />
            {target?.name || "Choose a container"}
            <strong>:{port || detectedPort}</strong>
          </span>
        </div>
        <details className="route-advanced" open={strip || undefined}>
          <summary>Path forwarding</summary>
          <label className="inline-check">
            <input
              type="checkbox"
              name="strip_prefix"
              checked={strip}
              onChange={(e) => setStrip(e.target.checked)}
            />
            Remove the path prefix before forwarding
          </label>
          <p className="small muted">
            {strip
              ? `A request to ${previewPath.replace(/\/$/, "")}/health reaches the container as /health.`
              : "The container receives the original request path, unchanged."}
          </p>
        </details>
        <div className="route-editor-footer">
          <p className="small muted">
            Point this hostname’s DNS A/AAAA record to your server.
          </p>
          <div className="form-actions">
            <button type="button" onClick={onCancel}>
              Cancel
            </button>
            <button type="submit" className="primary" disabled={!available}>
              {route ? "Save route" : "Connect domain"}
            </button>
          </div>
        </div>
      </AsyncForm>
    </section>
  );
}
