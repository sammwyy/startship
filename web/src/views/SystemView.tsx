import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ErrorMessage, Loading } from "../components/ui";
import { useSession } from "../providers/SessionProvider";
import { useUI } from "../providers/UIProvider";
import { ApiError, request } from "../services/api";
import { queries } from "../services/queries";
import type {
  SystemVersion,
  UpdateInfo,
  UpdateRequest,
} from "../services/types";

export function SystemView() {
  const { user } = useSession();
  const ui = useUI();
  const client = useQueryClient();
  const [tracking, setTracking] = useState<
    SystemVersion & { previousAttempt?: string }
  >();
  const [startedAt, setStartedAt] = useState(0);
  const [timedOut, setTimedOut] = useState(false);
  const [installed, setInstalled] = useState<string>();
  const status = useQuery(queries.updates());
  const info = status.data;
  const version = useQuery(queries.version());
  const refresh = useMutation({
    mutationFn: () => request<UpdateInfo>("system/update?refresh=1"),
    onSuccess: (data) => client.setQueryData(queries.updates().queryKey, data),
  });
  const update = useMutation({
    mutationFn: (target: string) =>
      request<UpdateRequest>("system/update", { version: target }),
    onSuccess: () => {
      setTracking({
        version: info!.current,
        commit: info!.commit,
        previousAttempt: info!.update?.started,
      });
      setStartedAt(Date.now());
      setTimedOut(false);
      setInstalled(undefined);
      void client.invalidateQueries(queries.updates());
    },
  });
  useEffect(() => {
    if (
      !tracking &&
      !installed &&
      (info?.pending || info?.update?.state === "running")
    ) {
      setTracking({
        version: info.current,
        commit: info.commit,
        previousAttempt: info.pending ? info.update?.started : undefined,
      });
      setStartedAt(Date.now());
      setTimedOut(false);
    }
  }, [tracking, installed, info]);
  useEffect(() => {
    if (!tracking || !version.data) return;
    if (
      version.data.version !== tracking.version ||
      version.data.commit !== tracking.commit
    ) {
      setInstalled(version.data.version);
      setTracking(undefined);
      void client.invalidateQueries(queries.updates());
    }
  }, [tracking, version.data, client]);
  useEffect(() => {
    if (!tracking) return;
    const timer = window.setTimeout(
      () => setTimedOut(true),
      Math.max(0, 15 * 60 * 1000 - (Date.now() - startedAt)),
    );
    return () => clearTimeout(timer);
  }, [tracking, startedAt]);
  useEffect(() => {
    if (
      tracking &&
      info?.update?.state === "failed" &&
      !info.pending &&
      info.update.started !== tracking.previousAttempt
    )
      setTracking(undefined);
  }, [tracking, info]);
  const running = info?.update?.state === "running";
  const busy = !!tracking || !!info?.pending || running || update.isPending;
  const canUpdate =
    !!user?.platform_admin &&
    !!info?.self_update &&
    !!info.update_available &&
    !info.check_error &&
    !busy;
  function confirmUpdate() {
    if (!info?.latest || !canUpdate) return;
    const target = info.latest;
    ui.dialog({
      title: `Update Startship to ${target}?`,
      description:
        "This updates both the daemon and CLI, then restarts the daemon. The dashboard may briefly disconnect. Existing application containers keep running.",
      submit: "Update and restart",
      onSubmit: async () => {
        await update.mutateAsync(target);
      },
    });
  }
  const missingAPI =
    status.error instanceof ApiError && status.error.status === 404;
  return (
    <>
      {!info ? (
        missingAPI ? (
          <p className="panel panel-pad">
            Update checks are not supported by this daemon. Upgrade Startship on
            the server to enable them.
          </p>
        ) : status.isPending ? (
          <Loading />
        ) : (
          <ErrorMessage error={status.error} />
        )
      ) : (
        <section
          className="panel panel-pad update-panel"
          aria-label="Startship version"
        >
          <div className="section-head">
            <div>
              <h2>Startship {info.current}</h2>
              <p className="small">
                {info.development
                  ? "Development build"
                  : info.update_available
                    ? `${info.latest} is available`
                    : info.latest
                      ? "You're up to date"
                      : "Latest version unavailable"}
              </p>
            </div>
            <button
              disabled={refresh.isPending || !!tracking}
              onClick={() => refresh.mutate()}
            >
              {refresh.isPending ? "Checking…" : "Check for updates"}
            </button>
          </div>
          <dl className="kv">
            <dt>Installed version</dt>
            <dd>{info.current}</dd>
            <dt>Commit</dt>
            <dd>
              <code>{info.commit || "Unknown"}</code>
            </dd>
            <dt>Latest release</dt>
            <dd>{info.latest || "Not checked"}</dd>
            <dt>Last checked</dt>
            <dd>
              {info.checked_at
                ? new Date(info.checked_at).toLocaleString()
                : "Not checked"}
            </dd>
          </dl>
          <ErrorMessage
            error={
              info.check_error || refresh.error || (!tracking && status.error)
            }
          />
          {info.development && (
            <p className="note">
              Development builds do not automatically install tagged releases.
              Install a release build to enable updates.
            </p>
          )}
          {!info.self_update && (
            <p className="note">
              Self-update is not configured. Use{" "}
              <code>sudo startship update</code> on the server, or redeploy your
              container with the new image.
            </p>
          )}
          {!user?.platform_admin && (
            <p className="note">Only Platform Admins can update Startship.</p>
          )}
          {info.update?.state === "failed" && (
            <ErrorMessage
              error={`Update to ${info.update.version} failed: ${info.update.error || "Check the updater logs on the server."}`}
            />
          )}
          {busy && !installed && (
            <p className="note" role="status">
              {timedOut
                ? "The update has not been confirmed after 15 minutes. Check the updater logs on the server, then check again."
                : version.isError || status.isError
                  ? "Waiting for the daemon to reconnect…"
                  : running
                    ? `Installing ${info.update?.version}… Waiting for the daemon to restart.`
                    : "Update queued. Waiting for the daemon to restart…"}
            </p>
          )}
          {installed && (
            <p className="note" role="status">
              Startship {installed} is running. Reload the dashboard to load its
              new assets.
            </p>
          )}
          <div className="form-actions">
            <button
              className="primary"
              disabled={!canUpdate}
              onClick={confirmUpdate}
            >
              Update daemon and CLI
            </button>
            {(installed || timedOut) && (
              <button onClick={() => window.location.reload()}>
                Reload dashboard
              </button>
            )}
            {info.release_url && /^https?:\/\//i.test(info.release_url) && (
              <a
                className="button"
                href={info.release_url}
                target="_blank"
                rel="noreferrer"
              >
                Release notes
              </a>
            )}
          </div>
          {info.notes && (
            <details className="update-notes">
              <summary>What's new in {info.latest}</summary>
              <pre>{info.notes}</pre>
            </details>
          )}
        </section>
      )}
    </>
  );
}
