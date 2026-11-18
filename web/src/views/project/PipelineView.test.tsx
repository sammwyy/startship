import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PipelineTemplate, Project } from "../../services/types";
import { PipelineView } from "./PipelineView";

const state = vi.hoisted(() => ({
  editable: true,
  refresh: vi.fn(),
  notify: vi.fn(),
  request: vi.fn(),
  project: {
    id: "p1",
    name: "Storefront",
    workspace_id: "w1",
    build_type: "auto",
  } as Project,
}));
vi.mock("../../layouts/ProjectLayout", () => ({
  useProject: () => ({ ...state, containers: [], releases: [], routes: [] }),
}));
vi.mock("../../providers/SessionProvider", () => ({
  useSession: () => ({ can: () => state.editable }),
}));
vi.mock("../../providers/UIProvider", () => ({
  useUI: () => ({ notify: state.notify }),
}));
vi.mock("../../services/api", () => ({ request: state.request }));
const templates: PipelineTemplate[] = [
  {
    id: "vite",
    name: "Vite",
    icon: "vite",
    category: "Frontend",
    description: "Vite SPA built with Node and served as static files",
    pipeline: {
      build: { image: "node:22-alpine", commands: ["npm ci", "npm run build"] },
      test: { expect_exit_code: 0 },
      deploy: { static: true, artifact: "dist", port: 3000 },
    },
  },
  {
    id: "go",
    name: "Go",
    icon: "go",
    category: "Backend",
    description: "Compile a Go binary",
    pipeline: {
      build: { image: "golang:1.26" },
      test: { expect_exit_code: 0 },
      deploy: { port: 8080 },
    },
  },
];
const staticDeploy = {
  static: true,
  artifact: "dist",
  image: "",
  command: "",
  port: 0,
  health_path: "",
  expect_status: 0,
};
function show() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter>
        <PipelineView />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
describe("project pipeline", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    state.editable = true;
    state.project = {
      id: "p1",
      name: "Storefront",
      workspace_id: "w1",
      build_type: "auto",
    };
    HTMLDialogElement.prototype.showModal = function () {
      this.setAttribute("open", "");
    };
    HTMLDialogElement.prototype.close = function () {
      this.removeAttribute("open");
    };
    state.request.mockImplementation(async (path: string) =>
      path === "pipeline/templates" ? templates : undefined,
    );
  });
  it("shows automatic build/runtime notes but keeps tests and overrides editable", async () => {
    show();
    expect(
      await screen.findAllByText("Detected per container on each deployment"),
    ).toHaveLength(2);
    expect(screen.queryByLabelText("Build image")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Test image")).toBeEnabled();
    expect(screen.getByLabelText("Port override (0 = detect)")).toHaveValue(0);
    expect(state.request).not.toHaveBeenCalledWith(
      "detect",
      expect.anything(),
      expect.anything(),
    );
  });
  it("fills stages from a template and saves only pipeline configuration", async () => {
    show();
    fireEvent.click(
      await screen.findByRole("button", { name: "Change template" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Use Vite" }));
    expect(screen.getByLabelText("Build image")).toHaveValue("node:22-alpine");
    expect(screen.getByLabelText("Build commands")).toHaveValue(
      "npm ci\nnpm run build",
    );
    expect(screen.getByLabelText("Artifact path")).toHaveValue("dist");
    expect(
      screen.getByLabelText("Serve artifact as static files"),
    ).toBeChecked();
    fireEvent.change(screen.getByLabelText("Expected output contains"), {
      target: { value: "passed" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save pipeline" }));
    await waitFor(() =>
      expect(state.request).toHaveBeenCalledWith(
        "projects/p1",
        {
          build_type: "vite",
          pipeline: {
            ...templates[0].pipeline,
            deploy: staticDeploy,
            test: { expect_exit_code: 0, expect_output: "passed" },
          },
        },
        "PATCH",
      ),
    );
    expect(state.refresh).toHaveBeenCalled();
    await waitFor(() => expect(state.notify).toHaveBeenCalled());
  });
  it("disables static runtime settings and clears them on save", async () => {
    state.project.build_type = "vite";
    state.project.pipeline = {
      ...templates[0].pipeline,
      deploy: {
        static: true,
        artifact: "dist",
        image: "node:22-alpine",
        command: "npm start",
        port: 3000,
        health_path: "/health",
        expect_status: 201,
      },
    };
    show();
    expect(
      await screen.findByText("Served directly by Startship"),
    ).toBeVisible();
    for (const label of [
      "Runtime image",
      "Start command",
      "HTTP port",
      "Health check path",
      "Expected HTTP status",
    ]) {
      expect(screen.getByLabelText(label)).toBeDisabled();
    }
    expect(screen.getByLabelText("Artifact path")).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Save pipeline" }));
    await waitFor(() =>
      expect(state.request).toHaveBeenCalledWith(
        "projects/p1",
        {
          build_type: "vite",
          pipeline: {
            ...templates[0].pipeline,
            deploy: staticDeploy,
          },
        },
        "PATCH",
      ),
    );
    fireEvent.click(screen.getByLabelText("Serve artifact as static files"));
    expect(
      screen.queryByText("Served directly by Startship"),
    ).not.toBeInTheDocument();
    expect(screen.getByLabelText("HTTP port")).toBeEnabled();
    expect(screen.getByLabelText("Start command")).toHaveValue("npm start");
  });
  it("filters templates by search and category", async () => {
    show();
    fireEvent.click(
      await screen.findByRole("button", { name: "Change template" }),
    );
    const modal = screen.getByRole("dialog");
    fireEvent.change(
      within(modal).getByRole("textbox", { name: "Search templates" }),
      { target: { value: "vite" } },
    );
    expect(
      within(modal).getByRole("button", { name: "Use Vite" }),
    ).toBeVisible();
    expect(
      within(modal).queryByRole("button", { name: "Use Go" }),
    ).not.toBeInTheDocument();
    fireEvent.click(within(modal).getByRole("button", { name: "Backend" }));
    expect(
      within(modal).getByText("No templates match your search."),
    ).toBeVisible();
    fireEvent.click(
      within(modal).getByRole("button", { name: "Close templates" }),
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("detects on click and prefers the resolved pipeline", async () => {
    state.request.mockImplementation(async (path: string) =>
      path === "pipeline/templates"
        ? templates
        : path === "detect"
          ? [
              {
                root_dir: ".",
                template: "vite",
                pipeline: {
                  ...templates[0].pipeline,
                  build: {
                    image: "node:20-alpine",
                    commands: ["pnpm install", "pnpm run build"],
                  },
                },
              },
            ]
          : undefined,
    );
    show();
    fireEvent.click(await screen.findByRole("button", { name: "Detect" }));
    fireEvent.click(await screen.findByRole("button", { name: "Apply Vite" }));
    expect(screen.getByLabelText("Build image")).toHaveValue("node:20-alpine");
    fireEvent.click(screen.getByRole("button", { name: "Change template" }));
    expect(
      within(screen.getByRole("dialog")).getByText("Detected"),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Use Vite" }));
    expect(screen.getByLabelText("Build commands")).toHaveValue(
      "pnpm install\npnpm run build",
    );
  });
  it("reports detection failures and respects read-only access", async () => {
    state.request.mockImplementation(async (path: string) => {
      if (path === "detect") throw new Error("Source unavailable");
      return templates;
    });
    const view = show();
    fireEvent.click(await screen.findByRole("button", { name: "Detect" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Source unavailable",
    );
    view.unmount();
    state.editable = false;
    show();
    expect(
      await screen.findByRole("button", { name: "Save pipeline" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Change template" }),
    ).toBeDisabled();
    expect(
      screen.getByText(/your roles do not let you change it/),
    ).toBeVisible();
  });
});
