import { useState } from "react";
import type { Service, ServiceEngine } from "../services/types";
import { validServiceName } from "../services/services";
import { AsyncForm, Field } from "./ui";
import { ServicePicker } from "./ServiceUI";
import { EngineIcon } from "./EngineIcon";
import { Icon } from "./Icon";

export function generateServiceSecret() {
  return Array.from(crypto.getRandomValues(new Uint8Array(24)), (v) =>
    v.toString(16).padStart(2, "0"),
  ).join("");
}
export function ServiceConfig({
  engine,
  service,
  onSave,
  onBack,
  disabled = false,
}: {
  engine: ServiceEngine;
  service?: Service;
  onSave: (body: Record<string, unknown>) => Promise<unknown>;
  onBack?: () => void;
  disabled?: boolean;
}) {
  const [name, setName] = useState(service?.name || engine.id),
    [version, setVersion] = useState(service?.version || engine.versions[0]);
  const [config, setConfig] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      (engine.fields || []).map((f) => [
        f.key,
        service?.config[f.key] ??
          (f.generate ? generateServiceSecret() : f.default),
      ]),
    ),
  );
  const [visible, setVisible] = useState<Record<string, boolean>>({}),
    [exposed, setExposed] = useState(!!service?.external_port),
    [port, setPort] = useState(
      service?.external_port ? String(service.external_port) : "",
    );
  const nameValid = validServiceName(name),
    portValid =
      !exposed ||
      (/^\d+$/.test(port) && Number(port) >= 1 && Number(port) <= 65535);
  return (
    <AsyncForm
      className="service-config"
      disabled={disabled}
      onSubmit={async () => {
        await onSave({
          ...(!service ? { name, engine: engine.id } : {}),
          version,
          config,
          external_port: exposed ? Number(port) : 0,
        });
      }}
    >
      <div className="service-config-body">
        <div className="service-engine-summary">
          <EngineIcon engine={engine.id} />
          <div>
            <strong>{engine.name}</strong>
            <span>
              {engine.image} · Port {engine.port}
            </span>
          </div>
          <span className="service-category">{engine.category}</span>
        </div>
        {service && (
          <div className="service-notice">
            <Icon name="history" />
            <span>
              The container will be recreated. Your persistent data is kept.
              Credential changes may require changes inside an initialized
              database.
            </span>
          </div>
        )}
        <div className="service-form-grid">
          <div>
            <Field
              label="Service name"
              name="name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              maxLength={63}
              disabled={!!service}
              aria-invalid={!nameValid}
            />
            {!nameValid && (
              <span className="service-validation">
                Start with a lowercase letter; use a–z, 0–9, _ or - (up to 63
                characters).
              </span>
            )}
          </div>
          <ServicePicker
            label="Version"
            value={version}
            options={engine.versions.map((v) => ({ value: v, label: v }))}
            onChange={setVersion}
            disabled={disabled}
          />
        </div>
        {!!engine.fields?.length && (
          <>
            <div className="service-section-label">CONFIGURATION</div>
            <div className="service-form-grid">
              {engine.fields.map((field) => (
                <div className="service-config-field" key={field.key}>
                  <label>
                    {field.label === field.key
                      ? field.key[0].toUpperCase() + field.key.slice(1)
                      : field.label}
                    <div className="secret-input">
                      <input
                        name={field.key}
                        required={field.required}
                        type={
                          field.type === "password" && !visible[field.key]
                            ? "password"
                            : field.type === "number"
                              ? "number"
                              : "text"
                        }
                        value={config[field.key] || ""}
                        onChange={(e) =>
                          setConfig({ ...config, [field.key]: e.target.value })
                        }
                        autoComplete="off"
                      />
                      {field.type === "password" && (
                        <>
                          <button
                            type="button"
                            aria-label={`${visible[field.key] ? "Hide" : "Show"} ${field.label}`}
                            onClick={() =>
                              setVisible({
                                ...visible,
                                [field.key]: !visible[field.key],
                              })
                            }
                          >
                            <Icon name="eye" />
                          </button>
                          <button
                            type="button"
                            aria-label={`Regenerate ${field.label}`}
                            onClick={() =>
                              setConfig({
                                ...config,
                                [field.key]: generateServiceSecret(),
                              })
                            }
                          >
                            <Icon name="bolt" />
                          </button>
                        </>
                      )}
                    </div>
                  </label>
                  {field.help && (
                    <span className="small muted">{field.help}</span>
                  )}
                </div>
              ))}
            </div>
          </>
        )}
        <div className="service-exposure">
          <div>
            <strong>Expose to the internet</strong>
            <p>
              Publish a host port. Keep this off for private project
              connections.
            </p>
          </div>
          <button
            type="button"
            className="service-switch"
            role="switch"
            aria-label="Expose to the internet"
            aria-checked={exposed}
            onClick={() => setExposed(!exposed)}
          >
            <span />
          </button>
        </div>
        {exposed && (
          <div>
            <Field
              label="External port"
              name="external_port"
              type="number"
              min={1}
              max={65535}
              required
              value={port}
              onChange={(e) => setPort(e.target.value)}
              placeholder="e.g. 16379"
            />
            {!portValid && (
              <span className="service-validation">
                Choose an unused port between 1 and 65535.
              </span>
            )}
            <p className="service-validation">
              Public ports are reachable from outside your server. Configure
              authentication and firewall rules before exposing data.
            </p>
          </div>
        )}
        <div className="service-persistence">
          <Icon name={engine.data_path ? "hard-drive" : "box"} />
          <span>
            <strong>
              {engine.data_path ? "Persistent by default" : "No persistence"}
            </strong>
            <span>
              {engine.data_path
                ? `Data lives on a persistent volume at ${engine.data_path}. Deleting this service keeps its data unless you choose otherwise.`
                : "Memcached is an in-memory cache. Its data is not stored on a volume."}
            </span>
          </span>
        </div>
      </div>
      <div className="service-modal-foot">
        {onBack ? (
          <button type="button" onClick={onBack}>
            <Icon name="back" />
            Back to catalog
          </button>
        ) : (
          <span />
        )}
        <button
          className="primary"
          disabled={!nameValid || !portValid || disabled}
        >
          <Icon name={service ? "check" : "rocket"} />
          {service ? "Save changes" : "Deploy service"}
        </button>
      </div>
    </AsyncForm>
  );
}
