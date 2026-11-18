import { Link } from "react-router-dom";
import { useProject } from "../../layouts/ProjectLayout";
import { ProjectHeading } from "../../components/ProjectHeading";
import { Icon } from "../../components/Icon";
import { Badge } from "../../components/ui";
import { useQuery } from "@tanstack/react-query";
import { queries } from "../../services/queries";
import { TemplateIcon } from "../../components/TemplateIcon";
export function ContainersView() {
  const { project, containers } = useProject();
  const { data: templates } = useQuery(queries.pipelineTemplates());
  return (
    <>
      <ProjectHeading title="Containers" />
      <div className="section-head">
        <h2>Detected containers</h2>
        <span className="small muted">Managed together by the project</span>
      </div>
      <div className="panel">
        {containers.map((c) => (
          <div className="container-row" key={c.id}>
            <span className="card-icon">
              <TemplateIcon
                slug={
                  templates?.find((t) => t.id === c.build_type)?.icon ||
                  "custom"
                }
              />
            </span>
            <div className="container-copy">
              <strong>{c.name}</strong>
              <span>
                {!c.root_dir || c.root_dir === "."
                  ? "/ (repo root)"
                  : c.root_dir}{" "}
                ·{" "}
                {templates?.find((t) => t.id === c.build_type)?.name ||
                  c.build_type}{" "}
                · :{c.port}
              </span>
            </div>
            <Badge status={c.status} />
            <Link className="button" to={`/project/${project.id}/logs/${c.id}`}>
              <Icon name="terminal" />
              Logs
            </Link>
          </div>
        ))}
        {!containers.length && (
          <p className="panel-pad small muted">
            No containers yet. Deploy the project; Startship detects and
            launches them automatically.
          </p>
        )}
      </div>
    </>
  );
}
