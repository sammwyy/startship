import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { RouteEditor } from "./RouteEditor";
import type {
  Container,
  DomainRoute,
  WorkspaceDomain,
} from "../services/types";

const web: Container = {
  id: "web",
  name: "website",
  project_id: "p",
  port: 3000,
};
const api: Container = {
  id: "api",
  name: "backend",
  project_id: "p",
  port: 8080,
};
const domain: WorkspaceDomain = {
  id: "d",
  name: "example.com",
  apex: true,
  subdomains: true,
  routes: 0,
};
const route: DomainRoute = {
  id: "r",
  host: "api.example.com",
  path_prefix: "/api",
  container_id: "api",
  port: 9000,
  strip_prefix: true,
};

function setup(
  options: {
    containers?: Container[];
    domains?: WorkspaceDomain[];
    route?: DomainRoute;
    admin?: boolean;
    onSave?: () => Promise<void>;
  } = {},
) {
  const onSave = options.onSave || vi.fn().mockResolvedValue(undefined);
  render(
    <RouteEditor
      containers={[web]}
      domains={[domain]}
      onCancel={vi.fn()}
      {...options}
      onSave={onSave}
    />,
  );
  return { user: userEvent.setup(), onSave };
}

describe("domain routing editor", () => {
  it("lets contributors retain an existing external host without a misleading preview", async () => {
    const { user, onSave } = setup({
      route: { ...route, container_id: "web", host: "legacy.test", port: 0 },
    });
    expect(screen.getByLabelText("Domain")).toHaveValue("legacy.test");
    expect(
      screen.getByText(/outside the workspace’s registered domains/),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Hostname")).toBeDisabled();
    await user.clear(screen.getByLabelText("Path"));
    await user.type(screen.getByLabelText("Path"), "/status");
    expect(screen.getByText("legacy.test/status")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save route" }));
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({ host: "legacy.test", path_prefix: "/status" }),
    );
    await user.selectOptions(screen.getByLabelText("Domain"), "example.com");
    expect(
      screen.queryByText(/outside the workspace’s registered domains/),
    ).not.toBeInTheDocument();
  });
  it("allows retaining an existing host with no registered workspace domains", async () => {
    const { user, onSave } = setup({
      domains: [],
      route: { ...route, container_id: "web", host: "legacy.test" },
    });
    expect(screen.getByLabelText("Domain")).toHaveValue("legacy.test");
    await user.click(screen.getByRole("button", { name: "Save route" }));
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({ host: "legacy.test" }),
    );
  });
  it("automatically targets a single container and follows its detected port", async () => {
    const { user, onSave } = setup();
    expect(
      screen.queryByRole("combobox", { name: "Container" }),
    ).not.toBeInTheDocument();
    expect(screen.getByLabelText("Container port")).toHaveValue(3000);
    expect(screen.getByLabelText("Domain")).toHaveValue("example.com");
    await user.click(screen.getByRole("button", { name: "Connect domain" }));
    expect(onSave).toHaveBeenCalledWith({
      host: "example.com",
      container_id: "web",
      path_prefix: "/",
      port: 0,
      strip_prefix: false,
    });
  });
  it("updates the destination port when choosing a container and saves a custom port and wildcard path", async () => {
    const { user, onSave } = setup({ containers: [web, api] });
    await user.selectOptions(screen.getByLabelText("Container"), "api");
    expect(screen.getByLabelText("Container port")).toHaveValue(8080);
    await user.clear(screen.getByLabelText("Container port"));
    await user.type(screen.getByLabelText("Container port"), "9000");
    await user.selectOptions(screen.getByLabelText("Domain"), "*.example.com");
    await user.type(screen.getByLabelText("Subdomain"), "service");
    await user.clear(screen.getByLabelText("Path"));
    await user.type(screen.getByLabelText("Path"), "api");
    await user.click(screen.getByText("Path forwarding"));
    await user.click(
      screen.getByLabelText("Remove the path prefix before forwarding"),
    );
    expect(screen.getByText("service.example.com/api")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Connect domain" }));
    expect(onSave).toHaveBeenCalledWith({
      host: "service.example.com",
      container_id: "api",
      path_prefix: "/api",
      port: 9000,
      strip_prefix: true,
    });
  });
  it("preserves the target, host, port and path behavior when editing", async () => {
    const { user, onSave } = setup({ containers: [web, api], route });
    expect(screen.getByLabelText("Container")).toHaveValue("api");
    expect(screen.getByLabelText("Subdomain")).toHaveValue("api");
    expect(screen.getByLabelText("Container port")).toHaveValue(9000);
    expect(
      screen.getByLabelText("Remove the path prefix before forwarding"),
    ).toBeChecked();
    await user.click(screen.getByRole("button", { name: "Save route" }));
    expect(onSave).toHaveBeenCalledWith({
      host: route.host,
      container_id: "api",
      path_prefix: "/api",
      port: 9000,
      strip_prefix: true,
    });
  });
  it("requires a registered domain for ordinary users but allows a custom host for Platform Admins", async () => {
    const first = render(
      <RouteEditor
        containers={[web]}
        domains={[]}
        onSave={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(
      screen.getByRole("button", { name: "Connect domain" }),
    ).toBeDisabled();
    first.unmount();
    const { user, onSave } = setup({ domains: [], admin: true });
    await user.type(screen.getByLabelText("Hostname"), "custom.test");
    await user.click(screen.getByRole("button", { name: "Connect domain" }));
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({ host: "custom.test" }),
    );
  });
  it("keeps entered values and shows a failed save without closing the editor", async () => {
    const { user } = setup({
      onSave: vi.fn().mockRejectedValue(new Error("route already exists")),
    });
    await user.click(screen.getByRole("button", { name: "Connect domain" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "route already exists",
    );
    expect(screen.getByLabelText("Container port")).toHaveValue(3000);
  });
});
