import { describe, expect, it } from "vitest";
import {
  busyRelease,
  environmentData,
  projectSections,
  projectSummary,
  sortReleases,
  sourceData,
} from "./project";
import type { Container, Release } from "../services/types";

describe("project configuration", () => {
  it("keeps project-centred sidebar sections without user-managed apps", () => {
    expect(projectSections.map(([key]) => key)).toEqual([
      "overview",
      "deployments",
      "logs",
      "containers",
      "traffic",
      "domains",
      "source",
      "environment",
    ]);
  });
  it("parses environment values without losing equals signs or whitespace", () => {
    expect(environmentData("TOKEN=a=b\n#comment\nNAME= hello \n")).toEqual({
      TOKEN: "a=b",
      NAME: " hello ",
    });
    expect(() => environmentData("BAD NAME=value")).toThrow("KEY=VALUE");
    expect(() => environmentData("NO_SEPARATOR")).toThrow("KEY=VALUE");
  });
  it("does not let environment names mutate the object prototype", () => {
    const env = environmentData("__proto__=value");
    expect(Object.getPrototypeOf(env)).toBeNull();
    expect(env.__proto__).toBe("value");
  });
  it("leaves stored repository credentials alone when no replacement is entered", () => {
    const form = new FormData();
    form.set("git_url", "file:///repository");
    form.set("branch", "main");
    expect(sourceData(form)).toEqual({
      git_url: "file:///repository",
      branch: "main",
    });
    form.set("git_token", "replacement");
    expect(sourceData(form).git_token).toBe("replacement");
  });
  it("counts running containers live and sorts releases without mutating cached data", () => {
    expect(
      projectSummary(
        [{ status: "running" }, { status: "failed" }] as Container[],
        [{}],
      ),
    ).toBe("2 containers · 1 running · 1 domain route");
    const releases = [
      { id: "old", created: "2026-01-01", status: "running" },
      { id: "new", created: "2026-01-02", status: "building" },
    ] as Release[];
    expect(sortReleases(releases)[0].id).toBe("new");
    expect(releases[0].id).toBe("old");
    expect(busyRelease(releases[1])).toBe(true);
    expect(busyRelease(releases[0])).toBe(false);
  });
});
