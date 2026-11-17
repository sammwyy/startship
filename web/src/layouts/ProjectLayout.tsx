import { Outlet, useOutletContext, useParams } from "react-router-dom";
import { useQueryClient, useSuspenseQueries } from "@tanstack/react-query";
import { queries } from "../services/queries";
import { busyRelease, sortReleases } from "../lib/project";
import type {
  Container,
  DomainRoute,
  Project,
  Release,
} from "../services/types";

interface ProjectContext {
  project: Project;
  containers: Container[];
  releases: Release[];
  routes: DomainRoute[];
  refresh: () => Promise<void>;
}
export const useProject = () => useOutletContext<ProjectContext>();
export function ProjectLayout() {
  const id = useParams().projectId!,
    client = useQueryClient();
  const [
    { data: project },
    { data: allContainers },
    { data: rawReleases },
    { data: routes },
  ] = useSuspenseQueries({
    queries: [
      { ...queries.project(id), refetchInterval: 2000 },
      { ...queries.containers(id), refetchInterval: 2000 },
      { ...queries.releases(id), refetchInterval: 2000 },
      { ...queries.routes(id), refetchInterval: 2000 },
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
          containers,
          releases,
          routes,
          refresh,
        } satisfies ProjectContext
      }
    />
  );
}
