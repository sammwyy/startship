import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { useDashboard } from "../../layouts/DashboardLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { remove, request } from "../../services/api";
import {
  certificateExpiry,
  certificatesPath,
  useCertificates,
} from "../../services/certificates";
import type { ACMESettings, WorkspaceDomain } from "../../services/types";
import { Heading } from "../../components/Heading";
import { Icon } from "../../components/Icon";
import {
  AsyncForm,
  Badge,
  Empty,
  ErrorMessage,
  Field,
  Loading,
} from "../../components/ui";
import { HostPicker, pickedHost } from "../../components/HostPicker";
import "./WorkspaceCertificatesView.css";

const production = "https://acme-v02.api.letsencrypt.org/directory";
const staging = "https://acme-staging-v02.api.letsencrypt.org/directory";

export function WorkspaceCertificatesView() {
  const id = useParams().workspaceId!;
  const { workspace } = useDashboard();
  const { user, can } = useSession();
  const allowed =
    !!user?.platform_admin ||
    (workspace?.id === id && can("workspace.domains", workspace));
  const client = useQueryClient(),
    ui = useUI();
  const path = certificatesPath(id);
  const { data, isPending, error, liveError } = useCertificates(id, allowed);
  const domains = useQuery({
    queryKey: [`workspaces/${id}/domains`],
    queryFn: () =>
      request<{ domains: WorkspaceDomain[] }>(`workspaces/${id}/domains`),
    enabled: allowed,
  });
  const [mode, setMode] = useState<"acme" | "upload">();
  const refresh = () => client.invalidateQueries({ queryKey: [path] });
  return (
    <>
      <Heading
        kicker="Workspace"
        title="Certificates"
        subtitle="Manage TLS certificates for this workspace’s routes."
        actions={
          allowed && (
            <>
              <button
                onClick={() =>
                  setMode(mode === "upload" ? undefined : "upload")
                }
              >
                <Icon name="plus" />
                Upload certificate
              </button>
              <button
                className="primary"
                onClick={() => setMode(mode === "acme" ? undefined : "acme")}
              >
                <Icon name="shield-check" />
                Request Let’s Encrypt certificate
              </button>
            </>
          )
        }
      />
      <div className="workspace-certificates">
        {!allowed ? (
          <Empty title="Certificate access required">
            Ask a workspace administrator for the workspace.domains permission.
          </Empty>
        ) : (
          <>
            {mode === "acme" && domains.isPending && <Loading />}
            {mode && (mode === "upload" || domains.data) && (
              <CertificateForm
                key={`${id}-${mode}`}
                mode={mode}
                domains={domains.data?.domains || []}
                admin={!!user?.platform_admin}
                path={path}
                onCancel={() => setMode(undefined)}
                onSaved={async () => {
                  setMode(undefined);
                  await refresh();
                  ui.notify(
                    mode === "acme"
                      ? "Certificate requested"
                      : "Certificate uploaded",
                  );
                }}
              />
            )}
            <ErrorMessage error={error || liveError || domains.error} />
            {isPending ? (
              <Loading />
            ) : (
              data &&
              (data.certificates.length ? (
                <div className="panel table-panel certificate-table">
                  <table>
                    <thead>
                      <tr>
                        <th>Domains / issuer</th>
                        <th>Source</th>
                        <th>Expires</th>
                        <th>Status</th>
                        <th>Auto-renew</th>
                        <th>Actions</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.certificates.map((certificate) => (
                        <tr key={certificate.id}>
                          <td>
                            <strong className="certificate-names">
                              <Icon name="shield-check" />
                              {certificate.domains.join(", ")}
                            </strong>
                            <span className="small muted certificate-detail">
                              {certificate.issuer || "Awaiting issuance"}
                            </span>
                            {certificate.last_error && (
                              <span className="certificate-error" role="alert">
                                {certificate.last_error}
                              </span>
                            )}
                          </td>
                          <td>
                            {certificate.source === "acme"
                              ? "Let’s Encrypt"
                              : "Uploaded"}
                          </td>
                          <td>{certificateExpiry(certificate)}</td>
                          <td>
                            <Badge status={certificate.status} />
                          </td>
                          <td>
                            {certificate.source === "acme" &&
                            certificate.auto_renew
                              ? "Enabled"
                              : "Off"}
                          </td>
                          <td>
                            <div className="row-actions">
                              {certificate.source === "acme" && (
                                <button
                                  disabled={certificate.status === "issuing"}
                                  onClick={() =>
                                    ui.run(async () => {
                                      await request(
                                        `${path}/${certificate.id}/renew`,
                                        {},
                                      );
                                      await refresh();
                                      ui.notify("Renewal requested");
                                    })
                                  }
                                >
                                  Renew
                                </button>
                              )}
                              <button
                                className="danger"
                                aria-label={`Delete ${certificate.domains.join(", ")}`}
                                onClick={() =>
                                  ui.dialog({
                                    title: "Delete certificate",
                                    description: `Delete the certificate for ${certificate.domains.join(", ")}? Routes using automatic selection may lose HTTPS.`,
                                    submit: "Delete certificate",
                                    onSubmit: async () => {
                                      await remove(`${path}/${certificate.id}`);
                                      await refresh();
                                      ui.notify("Certificate deleted");
                                    },
                                  })
                                }
                              >
                                <Icon name="trash" />
                                Delete
                              </button>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <Empty title="No certificates yet">
                  Request a Let’s Encrypt certificate or upload a PEM chain and
                  private key.
                </Empty>
              ))
            )}
          </>
        )}
        {user?.platform_admin && <AccountSettings />}
      </div>
    </>
  );
}

function CertificateForm({
  mode,
  domains,
  admin,
  path,
  onCancel,
  onSaved,
}: {
  mode: "acme" | "upload";
  domains: WorkspaceDomain[];
  admin: boolean;
  path: string;
  onCancel: () => void;
  onSaved: () => Promise<void>;
}) {
  const [hosts, setHosts] = useState<string[]>([]);
  const [cert, setCert] = useState("");
  const [key, setKey] = useState("");
  const [fileError, setFileError] = useState<unknown>();
  function hostname(form: FormData) {
    const host = pickedHost(form).trim().toLowerCase().replace(/\.$/, "");
    if (!host || host.includes("*") || !/^[a-z0-9.-]+$/.test(host))
      throw new Error("Enter a hostname without wildcards.");
    return host;
  }
  return (
    <section className="panel panel-pad certificate-form">
      <div className="certificate-form-title">
        <h3>
          {mode === "acme"
            ? "Request Let’s Encrypt certificate"
            : "Upload certificate"}
        </h3>
        <button
          className="icon-button"
          aria-label="Close certificate form"
          onClick={onCancel}
        >
          <Icon name="close" />
        </button>
      </div>
      <AsyncForm
        onSubmit={async (form) => {
          if (mode === "acme")
            await request(path, {
              source: "acme",
              domains: [...new Set([...hosts, hostname(form)])],
              auto_renew: form.has("auto_renew"),
            });
          else
            await request(path, {
              source: "upload",
              cert_pem: cert,
              key_pem: key,
            });
          setKey("");
          await onSaved();
        }}
      >
        {mode === "acme" ? (
          <>
            <p className="small muted">
              DNS must point at this server and port 80 must be publicly
              reachable for HTTP-01 validation. Wildcards are not supported.
            </p>
            <HostPicker domains={domains} admin={admin} />
            <div className="certificate-hosts">
              {hosts.map((host) => (
                <button
                  type="button"
                  key={host}
                  aria-label={`Remove ${host}`}
                  onClick={() => setHosts(hosts.filter((h) => h !== host))}
                >
                  {host}
                  <Icon name="close" />
                </button>
              ))}
              <button
                type="button"
                disabled={!admin && !domains.length}
                onClick={(event) => {
                  try {
                    const host = hostname(
                      new FormData(event.currentTarget.form!),
                    );
                    setHosts([...new Set([...hosts, host])]);
                    setFileError(undefined);
                  } catch (err) {
                    setFileError(err);
                  }
                }}
              >
                <Icon name="plus" />
                Add hostname
              </button>
            </div>
            <label className="inline-check">
              <input type="checkbox" name="auto_renew" defaultChecked />
              Automatically renew before expiry
            </label>
          </>
        ) : (
          <div className="certificate-pem-grid">
            {(
              [
                ["Certificate chain PEM", cert, setCert],
                ["Private key PEM", key, setKey],
              ] as const
            ).map(([label, value, setValue]) => (
              <div key={label}>
                <label>
                  {label}
                  <textarea
                    aria-label={label}
                    value={value}
                    onChange={(event) => setValue(event.target.value)}
                    required
                    rows={6}
                    spellCheck={false}
                    autoComplete="off"
                  />
                </label>
                <label className="certificate-file">
                  Choose {label.toLowerCase()} file
                  <input
                    type="file"
                    accept=".pem,.crt,.cer,.key,text/plain"
                    onChange={async (event) => {
                      const file = event.target.files?.[0];
                      if (!file) return;
                      try {
                        setValue(await file.text());
                        setFileError(undefined);
                      } catch (err) {
                        setFileError(err);
                      }
                    }}
                  />
                </label>
              </div>
            ))}
          </div>
        )}
        <ErrorMessage error={fileError} />
        <div className="form-actions">
          <button type="button" onClick={onCancel}>
            Cancel
          </button>
          <button
            className="primary"
            disabled={mode === "acme" && !admin && !domains.length}
          >
            {mode === "acme" ? "Request certificate" : "Upload certificate"}
          </button>
        </div>
      </AsyncForm>
    </section>
  );
}

function AccountSettings() {
  const ui = useUI(),
    client = useQueryClient();
  const { data, error } = useQuery({
    queryKey: ["system/acme"],
    queryFn: () => request<ACMESettings>("system/acme"),
  });
  return (
    <details className="panel panel-pad certificate-settings">
      <summary>
        Node ACME account settings{" "}
        <span className="small muted">Platform Admin</span>
      </summary>
      <ErrorMessage error={error} />
      {data && (
        <AccountForm
          key={`${data.email}-${data.directory_url}`}
          settings={data}
          onSave={async (settings) => {
            await request("system/acme", settings, "PUT");
            await client.invalidateQueries({ queryKey: ["system/acme"] });
            ui.notify("ACME settings saved");
          }}
        />
      )}
    </details>
  );
}

function AccountForm({
  settings,
  onSave,
}: {
  settings: ACMESettings;
  onSave: (settings: ACMESettings) => Promise<void>;
}) {
  const [directory, setDirectory] = useState(settings.directory_url);
  return (
    <AsyncForm
      onSubmit={(form) =>
        onSave({ email: String(form.get("email")), directory_url: directory })
      }
    >
      <p className="small muted">
        Applies to certificate requests across this node. Staging certificates
        are not trusted by browsers.
      </p>
      <div className="certificate-account-grid">
        <Field
          label="Account email"
          name="email"
          type="email"
          required
          defaultValue={settings.email}
        />
        <Field
          label="Directory URL"
          name="directory_url"
          type="url"
          required
          value={directory}
          onChange={(event) => setDirectory(event.target.value)}
        />
      </div>
      <div className="form-actions">
        <button type="button" onClick={() => setDirectory(production)}>
          Let’s Encrypt production
        </button>
        <button type="button" onClick={() => setDirectory(staging)}>
          Let’s Encrypt staging
        </button>
        <button className="primary">Save ACME settings</button>
      </div>
    </AsyncForm>
  );
}
