import { useState } from "react";
import { Link } from "react-router-dom";
import type { Project, Service, ServiceEngine } from "../services/types";
import {
  connectionEnv,
  envPrefix,
  validServiceName,
} from "../services/services";
import { request } from "../services/api";
import { EnvPreview, ServiceModal, ServicePicker } from "./ServiceUI";
import { AsyncForm, Badge, Field } from "./ui";
import { EngineIcon } from "./EngineIcon";
import { Icon } from "./Icon";

export function ConnectServiceModal({
  workspaceId,
  services,
  engines,
  projects,
  project,
  service,
  onClose,
  onSaved,
}: {
  workspaceId: string;
  services: Service[];
  engines: ServiceEngine[];
  projects: Project[];
  project?: Project;
  service?: Service;
  onClose: () => void;
  onSaved: () => Promise<unknown>;
}) {
  const [selected, setSelected] = useState(service?.id || ""),
    [projectId, setProjectId] = useState(project?.id || ""),
    [alias, setAlias] = useState(service?.name || "");
  const current = services.find((s) => s.id === selected),
    engine =
      current?.definition || engines.find((e) => e.id === current?.engine),
    eligible = projects.filter(
      (p) => !current?.attachments.some((a) => a.project_id === p.id),
    );
  const conflict = services.some((s) =>
    s.attachments.some(
      (a) =>
        a.project_id === projectId && envPrefix(a.alias) === envPrefix(alias),
    ),
  );
  const valid =
    !!current &&
    eligible.some((p) => p.id === projectId) &&
    validServiceName(alias) &&
    !conflict;
  return (
    <ServiceModal
      title={project ? "Connect a service" : "Attach to a project"}
      description="Share a private connection. Your application receives the environment variables on its next deploy."
      onClose={onClose}
    >
      <AsyncForm
        onSubmit={async () => {
          await request(`services/${selected}/attachments`, {
            project_id: projectId,
            alias,
          });
          await onSaved();
          onClose();
        }}
      >
        <div className="service-config-body">
          {!service && (
            <div
              className="connect-options"
              role="group"
              aria-label="Available services"
            >
              {services.map((s) => (
                <button
                  type="button"
                  className={`connect-option ${selected === s.id ? "selected" : ""}`}
                  key={s.id}
                  disabled={
                    !!project &&
                    s.attachments.some((a) => a.project_id === project.id)
                  }
                  onClick={() => {
                    setSelected(s.id);
                    setAlias(s.name);
                  }}
                >
                  <EngineIcon engine={s.engine} />
                  <span>
                    <strong>{s.name}</strong>
                    <span>
                      {s.engine} · {s.version}
                    </span>
                    <span>
                      Attached to:{" "}
                      {s.attachments
                        .map(
                          (a) =>
                            projects.find((p) => p.id === a.project_id)?.name ||
                            a.project_id,
                        )
                        .join(", ") || "No projects yet"}
                    </span>
                    {project &&
                      s.attachments.some(
                        (a) => a.project_id === project.id,
                      ) && <span>Connected to this project</span>}
                  </span>
                  <Badge status={s.status} />
                  {selected === s.id && <Icon name="check" />}
                </button>
              ))}
              {!services.length && (
                <p>
                  No workspace services are available. Deploy a new service to
                  get started.
                </p>
              )}
            </div>
          )}
          {service && (
            <div className="service-engine-summary">
              <EngineIcon engine={service.engine} />
              <div>
                <strong>{service.name}</strong>
                <span>
                  Attached to:{" "}
                  {service.attachments
                    .map(
                      (a) =>
                        projects.find((p) => p.id === a.project_id)?.name ||
                        a.project_id,
                    )
                    .join(", ") || "No projects yet"}
                </span>
              </div>
              <Badge status={service.status} />
            </div>
          )}
          {!project && (
            <ServicePicker
              label="Project"
              value={projectId}
              options={eligible.map((p) => ({ value: p.id, label: p.name }))}
              onChange={setProjectId}
            />
          )}
          <Field
            label="Connection alias"
            name="alias"
            required
            disabled={!current}
            value={alias}
            maxLength={63}
            onChange={(e) => setAlias(e.target.value)}
            placeholder="e.g. primary_db"
            aria-invalid={!!alias && (!validServiceName(alias) || conflict)}
          />
          <span className="small muted">
            A lowercase DNS label, unique within the project. Use letters,
            numbers, _ or -.
          </span>
          {alias && (!validServiceName(alias) || conflict) && (
            <span className="service-validation">
              {conflict
                ? "This alias or environment prefix is already in use."
                : "Start with a lowercase letter. Use up to 63 lowercase letters, numbers, _ or -."}
            </span>
          )}
          {current && engine && alias ? (
            <EnvPreview env={connectionEnv(current, engine, alias)} canReveal />
          ) : (
            <p className="small muted">
              {current
                ? "Enter an alias to preview its connection environment."
                : "Choose a service to preview its connection environment."}
            </p>
          )}
          {!service && (
            <Link
              className="service-text-link"
              to={`/workspace/${workspaceId}/services`}
              onClick={onClose}
            >
              <Icon name="plus" />
              Deploy a new service
            </Link>
          )}
        </div>
        <div className="service-modal-foot">
          <button type="button" onClick={onClose}>
            Cancel
          </button>
          <button className="primary" disabled={!valid}>
            <Icon name="plug" />
            Connect service
          </button>
        </div>
      </AsyncForm>
    </ServiceModal>
  );
}
