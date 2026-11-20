import { Outlet, useOutletContext, useParams } from "react-router-dom";
import { useQueryClient, useSuspenseQueries } from "@tanstack/react-query";
import { queries } from "../services/queries";
import { busyRelease, sortReleases } from "../lib/project";
import { useLiveSubscription } from "../providers/LiveProvider";
import type {
  Container,
  DomainRoute,
  Project,
  Preview,
  Release,
} from "../services/types";

interface ProjectContext {
  project: Project;
  previews: Preview[];
  containers: Container[];
  releases: Release[];
  routes: DomainRoute[];
  refresh: () => Promise<void>;
}
export const useProject = () => useOutletContext<ProjectContext>();
export function ProjectLayout() {
  const id = useParams().projectId!,
    client = useQueryClient();
  useLiveSubscription({ topic: "project", project_id: id });
  const [
    { data: project },
    { data: allContainers },
    { data: rawReleases },
    { data: routes },
    { data: previews },
  ] = useSuspenseQueries({
    queries: [
      queries.project(id),
      queries.containers(id),
      queries.releases(id),
      queries.routes(id),
      queries.previews(id),
    ],
  });
  const releases = sortReleases(rawReleases);
  const containers = project.active_deployment
    ? allContainers.filter(
        (c) =>
          c.active_deployment ||
          (busyRelease(releases[0]) && c.release_id === releases[0].id),
      )
    : allContainers;
  const refresh = async () => {
    await client.invalidateQueries();
  };
  return (
    <Outlet
      context={
        {
          project,
          previews,
          containers,
          releases,
          routes,
          refresh,
        } satisfies ProjectContext
      }
    />
  );
}
