import type { Project } from "../services/types";
import { Field } from "./ui";
export function SourceFields({ project }: { project?: Project }) {
  return (
    <>
      <Field
        label="Project repository URL"
        name="git_url"
        type="url"
        defaultValue={project?.git_url}
        required
        placeholder="https://github.com/you/project"
      />
      <Field
        label="Shared branch"
        name="branch"
        defaultValue={project?.branch || "main"}
        required
      />
      <Field
        label="Repository token (leave blank to keep existing)"
        name="git_token"
        type="password"
        autoComplete="off"
      />
    </>
  );
}
