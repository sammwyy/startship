import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { useProject } from "../../layouts/ProjectLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { request } from "../../services/api";
import { queries } from "../../services/queries";
import type {
  DetectedPipeline,
  Pipeline,
  PipelineTemplate,
} from "../../services/types";
import { ProjectHeading } from "../../components/ProjectHeading";
import { AsyncForm, ErrorMessage, Field, Loading } from "../../components/ui";
import { Icon } from "../../components/Icon";
import { TemplateIcon } from "../../components/TemplateIcon";
import { PipelineTemplateModal } from "../../components/PipelineTemplateModal";

const emptyPipeline = (): Pipeline => ({
  build: { image: "alpine:3.23" },
  test: { expect_exit_code: 0 },
  deploy: { port: 0 },
});
const autoTemplate: PipelineTemplate = {
  id: "auto",
  name: "Auto-detect",
  description:
    "Detect the right pipeline for each container, on every deployment.",
  icon: "auto",
  category: "Automatic",
  pipeline: emptyPipeline(),
};
const lines = (value: string) => value.split("\n");
const tabs = ["build", "test", "deploy", "health"] as const;
const tabNames = ["Build", "Test", "Deploy", "Health check"];
const clean = (p: Pipeline): Pipeline => ({
  ...p,
  build: { ...p.build, commands: p.build.commands?.filter((s) => s.trim()) },
  test: {
    ...p.test,
    commands: p.test.commands?.filter((s) => s.trim()),
    expect_output: p.test.expect_output || "",
  },
  deploy: p.deploy.static
    ? {
        static: true,
        artifact: p.deploy.artifact,
        image: "",
        command: "",
        port: 0,
        health_path: "",
        expect_status: 0,
      }
    : p.deploy,
});

export function PipelineView() {
  const { project } = useProject();
  const { data, error, isPending } = useQuery(queries.pipelineTemplates());
  return (
    <>
      <ProjectHeading title="Pipeline" />
      {isPending ? (
        <Loading />
      ) : error ? (
        <ErrorMessage error={error} />
      ) : (
        <PipelineEditor key={project.id} templates={data!} />
      )}
    </>
  );
}
function PipelineEditor({ templates }: { templates: PipelineTemplate[] }) {
  const [params, setParams] = useSearchParams();
  const tab = tabs.find((value) => value === params.get("tab")) || "build";
  function selectTab(value: string) {
    setParams(
      (previous) => {
        previous.set("tab", value);
        return previous;
      },
      { replace: true },
    );
  }
  const { project, refresh } = useProject(),
    { can } = useSession(),
    ui = useUI();
  const editable = can("project.settings", project);
  const [strategy, setStrategy] = useState(project.build_type || "auto");
  const [pipeline, setPipeline] = useState<Pipeline>(() => {
    const base = structuredClone(
      project.pipeline ||
        templates.find((t) => t.id === project.build_type)?.pipeline ||
        emptyPipeline(),
    );
    if (!project.pipeline) {
      if (project.test_command) base.test.commands = [project.test_command];
      base.deploy.port =
        project.port ||
        (project.build_type === "auto" || !project.build_type
          ? 0
          : base.deploy.port);
    }
    return base;
  });
  const [modal, setModal] = useState(false),
    [detected, setDetected] = useState<DetectedPipeline[]>([]),
    [detecting, setDetecting] = useState(false),
    [hasDetected, setHasDetected] = useState(false),
    [detectError, setDetectError] = useState<unknown>();
  const auto = strategy === "auto",
    staticServing = !auto && !!pipeline.deploy.static,
    current =
      strategy === "auto"
        ? autoTemplate
        : templates.find((t) => t.id === strategy);
  const detectedPort = detected.find((d) => auto || d.template === strategy)
    ?.pipeline?.deploy.port;
  const portHint = detectedPort || current?.pipeline.deploy.port;
  function stage<K extends keyof Pipeline>(
    key: K,
    patch: Partial<Pipeline[K]>,
  ) {
    setPipeline((p) => ({ ...p, [key]: { ...p[key], ...patch } }));
  }
  function apply(template: PipelineTemplate, resolved?: Pipeline) {
    const match =
      detected.find(
        (d) =>
          d.template === template.id &&
          d.root_dir === (project.root_dir || "."),
      ) || detected.find((d) => d.template === template.id);
    setStrategy(template.id);
    setPipeline(
      structuredClone(resolved || match?.pipeline || template.pipeline),
    );
    setModal(false);
  }
  async function detect() {
    setDetecting(true);
    setDetectError(undefined);
    try {
      setDetected(
        await request<DetectedPipeline[]>(
          "detect",
          { project_id: project.id },
          "POST",
        ),
      );
      setHasDetected(true);
    } catch (e) {
      setDetectError(e);
    } finally {
      setDetecting(false);
    }
  }
  return (
    <div className="pipeline-page">
      {!editable && (
        <p className="note locked-note">
          You can view this section, but your roles do not let you change it.
        </p>
      )}
      <AsyncForm
        onSubmit={async () => {
          if (!editable) return;
          await request(
            `projects/${project.id}`,
            { build_type: strategy, pipeline: clean(pipeline) },
            "PATCH",
          );
          await refresh();
          ui.notify("Pipeline saved; changes apply on next deployment");
        }}
      >
        <section className="panel panel-pad pipeline-strategy">
          <fieldset disabled={!editable} className="pipeline-strategy-controls">
            <div className="section-head">
              <h2>Strategy</h2>
              <div className="pipeline-buttons">
                <button type="button" onClick={() => setModal(true)}>
                  Change template
                </button>
                <button type="button" disabled={detecting} onClick={detect}>
                  <Icon name="bolt" />
                  {detecting ? "Detecting…" : "Detect"}
                </button>
              </div>
            </div>
            <div className="pipeline-current">
              <TemplateIcon slug={current?.icon || "custom"} />
              <div>
                <strong>{current?.name || strategy}</strong>
                <p className="small muted">{current?.description}</p>
              </div>
              {auto && (
                <span className="pipeline-recommended">Recommended</span>
              )}
            </div>
            <ErrorMessage error={detectError} />
            {hasDetected && !detected.length && (
              <p className="small muted">
                No supported templates detected in this repository.
              </p>
            )}
            {!!detected.length && (
              <div className="pipeline-detections" aria-live="polite">
                <span className="small muted">Detected in your repository</span>
                {detected.map((d) => {
                  const t = templates.find((t) => t.id === d.template);
                  return (
                    <div key={d.root_dir}>
                      <TemplateIcon slug={t?.icon || "custom"} />
                      <span>
                        <strong>{t?.name || d.template}</strong>
                        <code>
                          {!d.root_dir || d.root_dir === "."
                            ? "Repository root"
                            : d.root_dir}
                        </code>
                      </span>
                      {project.build_type === d.template ? (
                        <div className="pipeline-in-use small muted">
                          <Icon name="check" />
                          In use
                        </div>
                      ) : (
                        <button
                          type="button"
                          onClick={() => t && apply(t, d.pipeline)}
                        >
                          Apply {t?.name || d.template}
                        </button>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </fieldset>
        </section>
        <div className="panel pipeline-editor-tabs">
          <div
            role="tablist"
            aria-label="Pipeline stages"
            className="pipeline-tabs"
          >
            {tabs.map((value, index) => {
              const configured =
                value === "health"
                  ? !!(
                      pipeline.deploy.health_path ||
                      pipeline.deploy.expect_status ||
                      pipeline.deploy.health_timeout
                    )
                  : value === "deploy"
                    ? !!(
                        pipeline.deploy.command ||
                        pipeline.deploy.static ||
                        pipeline.deploy.port
                      )
                    : value === "build"
                      ? !!(
                          pipeline.build.commands?.length ||
                          pipeline.build.dockerfile
                        )
                      : !!(
                          pipeline.test.commands?.length ||
                          pipeline.test.expect_output ||
                          pipeline.test.image ||
                          pipeline.test.expect_exit_code
                        );
              return (
                <button
                  key={value}
                  type="button"
                  role="tab"
                  id={`pipeline-tab-${value}`}
                  aria-controls={`pipeline-panel-${value}`}
                  aria-selected={tab === value}
                  tabIndex={tab === value ? 0 : -1}
                  onClick={() => selectTab(value)}
                  onKeyDown={(event) => {
                    let next = index;
                    if (event.key === "ArrowRight")
                      next = (index + 1) % tabs.length;
                    else if (event.key === "ArrowLeft")
                      next = (index + tabs.length - 1) % tabs.length;
                    else if (event.key === "Home") next = 0;
                    else if (event.key === "End") next = tabs.length - 1;
                    else return;
                    event.preventDefault();
                    selectTab(tabs[next]);
                    document
                      .getElementById(`pipeline-tab-${tabs[next]}`)
                      ?.focus();
                  }}
                >
                  <span>{index + 1}</span> {tabNames[index]}{" "}
                  {configured && (
                    <span
                      className="pipeline-tab-dot"
                      aria-label="Configured"
                    />
                  )}
                </button>
              );
            })}
          </div>
          <section
            hidden={tab !== "build"}
            role="tabpanel"
            id="pipeline-panel-build"
            aria-labelledby="pipeline-tab-build"
            className="pipeline-stage"
          >
            <header>
              <span className="pipeline-step">1</span>
              <div>
                <h2>Build</h2>
                <p>Prepare your application</p>
              </div>
              <Icon name="code" />
            </header>
            <fieldset disabled={!editable} className="pipeline-stage-body">
              {auto ? (
                <DetectedNote />
              ) : (
                <>
                  <Field
                    label="Build image"
                    placeholder="node:22-alpine"
                    value={pipeline.build.image || ""}
                    onChange={(e) => stage("build", { image: e.target.value })}
                  />
                  {strategy === "dockerfile" || pipeline.build.dockerfile ? (
                    <Field
                      label="Dockerfile path"
                      value={pipeline.build.dockerfile || ""}
                      required
                      onChange={(e) =>
                        stage("build", { dockerfile: e.target.value })
                      }
                    />
                  ) : (
                    <label>
                      Build commands
                      <textarea
                        rows={9}
                        placeholder="One command per line"
                        value={pipeline.build.commands?.join("\n") || ""}
                        onChange={(e) =>
                          stage("build", { commands: lines(e.target.value) })
                        }
                      />
                    </label>
                  )}
                  <p className="small muted pipeline-build-hint">
                    Runs in /app with your repository copied in.
                  </p>
                </>
              )}
            </fieldset>
          </section>
          <section
            hidden={tab !== "test"}
            role="tabpanel"
            id="pipeline-panel-test"
            aria-labelledby="pipeline-tab-test"
            className="pipeline-stage"
          >
            <header>
              <span className="pipeline-step">2</span>
              <div>
                <h2>Test</h2>
                <p>Verify before going live</p>
              </div>
              <Icon name="check" />
            </header>
            <fieldset disabled={!editable} className="pipeline-stage-body">
              {pipeline.build.dockerfile && !auto ? (
                <p className="small muted">
                  Tests run in the Dockerfile’s final image.
                </p>
              ) : (
                <Field
                  label="Test image"
                  placeholder="Same as build"
                  value={pipeline.test.image || ""}
                  onChange={(e) => stage("test", { image: e.target.value })}
                />
              )}
              <label>
                Test commands
                <textarea
                  rows={3}
                  placeholder="Optional"
                  value={pipeline.test.commands?.join("\n") || ""}
                  onChange={(e) =>
                    stage("test", { commands: lines(e.target.value) })
                  }
                />
              </label>
              <div className="split">
                <Field
                  label="Exit code"
                  type="number"
                  required
                  value={pipeline.test.expect_exit_code}
                  onChange={(e) =>
                    stage("test", { expect_exit_code: Number(e.target.value) })
                  }
                />
                <Field
                  label="Output contains"
                  placeholder="Optional"
                  value={pipeline.test.expect_output || ""}
                  onChange={(e) =>
                    stage("test", { expect_output: e.target.value })
                  }
                />
              </div>
            </fieldset>
          </section>
          <section
            hidden={tab !== "deploy"}
            role="tabpanel"
            id="pipeline-panel-deploy"
            aria-labelledby="pipeline-tab-deploy"
            className="pipeline-stage"
          >
            <header>
              <span className="pipeline-step">3</span>
              <div>
                <h2>Deploy</h2>
                <p>Serve & check your application</p>
              </div>
              <Icon name="rocket" />
            </header>
            <fieldset disabled={!editable} className="pipeline-stage-body">
              {auto ? (
                <DetectedNote />
              ) : pipeline.build.dockerfile ? (
                <p className="small muted">
                  Runtime image and start command come from your Dockerfile.
                </p>
              ) : (
                <>
                  <div className="split">
                    <Field
                      label="Runtime image"
                      disabled={staticServing}
                      placeholder="Same as build"
                      value={pipeline.deploy.image || ""}
                      onChange={(e) =>
                        stage("deploy", { image: e.target.value })
                      }
                    />
                    <Field
                      label="Artifact path"
                      placeholder="."
                      value={pipeline.deploy.artifact || ""}
                      onChange={(e) =>
                        stage("deploy", { artifact: e.target.value })
                      }
                    />
                  </div>
                  <label className="inline-check">
                    <input
                      type="checkbox"
                      checked={!!pipeline.deploy.static}
                      onChange={(e) =>
                        stage("deploy", { static: e.target.checked })
                      }
                    />
                    Serve artifact as static files
                  </label>
                  {staticServing && (
                    <p className="small muted">Served directly by Startship</p>
                  )}
                  <Field
                    label="Start command"
                    placeholder="npm start"
                    disabled={pipeline.deploy.static}
                    value={pipeline.deploy.command || ""}
                    onChange={(e) =>
                      stage("deploy", { command: e.target.value })
                    }
                  />
                </>
              )}
              <div className="pipeline-health-grid">
                <Field
                  label={auto ? "Port override" : "HTTP port"}
                  disabled={staticServing}
                  type="number"
                  min={0}
                  max={65535}
                  placeholder={portHint ? String(portHint) : "Auto"}
                  value={pipeline.deploy.port || ""}
                  onChange={(e) =>
                    stage("deploy", { port: Number(e.target.value) })
                  }
                />
              </div>
            </fieldset>
          </section>
          <section
            hidden={tab !== "health"}
            role="tabpanel"
            id="pipeline-panel-health"
            aria-labelledby="pipeline-tab-health"
            className="pipeline-stage"
          >
            <header>
              <span className="pipeline-step">4</span>
              <div>
                <h2>Health check</h2>
                <p>Wait until your application is ready to serve traffic</p>
              </div>
              <Icon name="check" />
            </header>
            <fieldset disabled={!editable} className="pipeline-stage-body">
              <p className="small muted">
                {staticServing
                  ? "Static files are served directly by Startship; container health checks do not apply."
                  : pipeline.deploy.health_path
                    ? `HTTP GET ${pipeline.deploy.health_path} — expect status ${pipeline.deploy.expect_status || 200}`
                    : `TCP connection on port ${pipeline.deploy.port || portHint || "auto-detected at deployment"}`}
              </p>
              <div className="split">
                <Field
                  label="Health check path"
                  disabled={staticServing}
                  placeholder="TCP check"
                  value={pipeline.deploy.health_path || ""}
                  onChange={(e) =>
                    stage("deploy", { health_path: e.target.value })
                  }
                />
                <Field
                  label="HTTP status"
                  disabled={staticServing}
                  type="number"
                  min={100}
                  max={599}
                  placeholder="200"
                  value={pipeline.deploy.expect_status || ""}
                  onChange={(e) =>
                    stage("deploy", {
                      expect_status: Number(e.target.value) || undefined,
                    })
                  }
                />
              </div>
              <Field
                label="Readiness timeout (seconds)"
                type="number"
                min={0}
                max={600}
                disabled={staticServing}
                placeholder="60 (default)"
                value={pipeline.deploy.health_timeout || ""}
                onChange={(e) =>
                  stage("deploy", { health_timeout: Number(e.target.value) })
                }
              />
            </fieldset>
          </section>
        </div>
        <div className="configuration-actions">
          <span>
            One pipeline for every container. Applies on the next deployment.
          </span>
          <button className="primary" disabled={!editable}>
            Save pipeline
          </button>
        </div>
      </AsyncForm>
      {modal && (
        <PipelineTemplateModal
          templates={[autoTemplate, ...templates]}
          current={strategy}
          detected={detected.map((d) => d.template)}
          onSelect={apply}
          onClose={() => setModal(false)}
        />
      )}
    </div>
  );
}
function DetectedNote() {
  return (
    <div className="pipeline-auto-note">
      <Icon name="bolt" />
      <div>
        <strong>Detected per container on each deployment</strong>
        <p className="small muted">
          Startship picks the image and commands from your source.
        </p>
      </div>
    </div>
  );
}
