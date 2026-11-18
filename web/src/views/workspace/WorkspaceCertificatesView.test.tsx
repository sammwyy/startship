import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UIProvider } from "../../providers/UIProvider";
import { request, remove } from "../../services/api";
import { WorkspaceCertificatesView } from "./WorkspaceCertificatesView";

let admin = true,
  allowed = true,
  populated = false;
vi.mock("../../providers/SessionProvider", () => ({
  useSession: () => ({ user: { platform_admin: admin }, can: () => allowed }),
}));
vi.mock("../../layouts/DashboardLayout", () => ({
  useDashboard: () => ({ workspace: { id: "w" } }),
}));
vi.mock("../../services/api", () => ({ request: vi.fn(), remove: vi.fn() }));
beforeEach(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute("open");
  };
  vi.clearAllMocks();
  admin = true;
  allowed = true;
  populated = false;
  vi.mocked(request).mockImplementation(async (path) => {
    if (path === "system/acme")
      return {
        email: "admin@example.com",
        directory_url: "https://ca.test/directory",
      };
    if (path.endsWith("/domains"))
      return {
        domains: [
          { id: "d", name: "example.com", apex: true, subdomains: true },
        ],
      };
    return {
      certificates: populated
        ? [
            {
              id: "c",
              domains: ["example.com"],
              source: "acme",
              issuer: "Test CA",
              status: "failed",
              last_error: "DNS lookup failed",
              not_after: "2099-01-01",
              auto_renew: true,
            },
          ]
        : [],
    };
  });
});
function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  render(
    <QueryClientProvider client={client}>
      <UIProvider>
        <MemoryRouter initialEntries={["/workspace/w/certificates"]}>
          <Routes>
            <Route
              path="/workspace/:workspaceId/certificates"
              element={<WorkspaceCertificatesView />}
            />
          </Routes>
        </MemoryRouter>
      </UIProvider>
    </QueryClientProvider>,
  );
  return userEvent.setup();
}
describe("workspace certificates", () => {
  it("shows the permission state without fetching certificates or exposing write actions", async () => {
    admin = false;
    allowed = false;
    setup();
    expect(screen.getByText("Certificate access required")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Upload certificate" }),
    ).not.toBeInTheDocument();
    expect(request).not.toHaveBeenCalled();
  });
  it("requests SANs from workspace claims with auto renewal and hides node settings from non-admins", async () => {
    admin = false;
    const user = setup();
    await screen.findByText("No certificates yet");
    await user.click(
      screen.getByRole("button", { name: /Request Let’s Encrypt/ }),
    );
    await user.click(screen.getByRole("button", { name: "Add hostname" }));
    await user.selectOptions(screen.getByLabelText("Domain"), "*.example.com");
    await user.type(screen.getByLabelText("Subdomain"), "app");
    await user.click(
      screen.getByRole("button", { name: "Request certificate" }),
    );
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("workspaces/w/certificates", {
        source: "acme",
        domains: ["example.com", "app.example.com"],
        auto_renew: true,
      }),
    );
    expect(screen.queryByText(/Node ACME account/)).not.toBeInTheDocument();
  });
  it("waits for domain claims before initializing the hostname picker", async () => {
    admin = false;
    const original = vi.mocked(request).getMockImplementation()!;
    let resolveDomains!: (value: unknown) => void;
    vi.mocked(request).mockImplementation((path, ...args) =>
      path.endsWith("/domains")
        ? new Promise((resolve) => {
            resolveDomains = resolve;
          })
        : original(path, ...args),
    );
    const user = setup();
    await user.click(
      screen.getByRole("button", { name: /Request Let’s Encrypt/ }),
    );
    expect(screen.queryByLabelText("Domain")).not.toBeInTheDocument();
    resolveDomains({ domains: [{ id: "d", name: "example.com", apex: true }] });
    expect(await screen.findByLabelText("Domain")).toHaveValue("example.com");
  });
  it("uploads a pasted PEM chain and key and closes the form on success", async () => {
    const user = setup();
    await screen.findByText("No certificates yet");
    await user.click(
      screen.getByRole("button", { name: "Upload certificate" }),
    );
    await user.type(
      screen.getByLabelText("Certificate chain PEM"),
      "test certificate PEM",
    );
    await user.type(screen.getByLabelText("Private key PEM"), "test key PEM");
    const section = screen
      .getByRole("heading", { name: "Upload certificate" })
      .closest("section")!;
    await user.click(
      within(section).getByRole("button", { name: "Upload certificate" }),
    );
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("workspaces/w/certificates", {
        source: "upload",
        cert_pem: "test certificate PEM",
        key_pem: "test key PEM",
      }),
    );
    await waitFor(() =>
      expect(
        screen.queryByLabelText("Private key PEM"),
      ).not.toBeInTheDocument(),
    );
  });
  it("rejects wildcard requests, and keeps upload input on server validation errors", async () => {
    const user = setup();
    await screen.findByText("No certificates yet");
    await user.click(
      screen.getByRole("button", { name: /Request Let’s Encrypt/ }),
    );
    await user.selectOptions(screen.getByLabelText("Domain"), "");
    await user.type(screen.getByLabelText("Hostname"), "*.example.com");
    await user.click(
      screen.getByRole("button", { name: "Request certificate" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "without wildcards",
    );
    await user.click(
      screen.getByRole("button", { name: "Upload certificate" }),
    );
    await user.type(
      screen.getByLabelText("Certificate chain PEM"),
      "invalid cert",
    );
    await user.type(screen.getByLabelText("Private key PEM"), "invalid key");
    vi.mocked(request).mockRejectedValueOnce(
      new Error("certificate and key do not match"),
    );
    const section = screen
      .getByRole("heading", { name: "Upload certificate" })
      .closest("section")!;
    await user.click(
      within(section).getByRole("button", { name: "Upload certificate" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "certificate and key do not match",
    );
    expect(screen.getByLabelText("Private key PEM")).toHaveValue("invalid key");
  });
  it("shows issuer, status and errors, and renews ACME certificates", async () => {
    populated = true;
    const user = setup();
    expect(await screen.findByText("Test CA")).toBeInTheDocument();
    expect(screen.getByText("DNS lookup failed")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Renew" }));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "workspaces/w/certificates/c/renew",
        {},
      ),
    );
  });
  it("keeps deletion conflict reasons visible in the confirmation dialog", async () => {
    populated = true;
    const user = setup();
    await screen.findByText("Test CA");
    vi.mocked(remove).mockRejectedValueOnce(
      new Error("certificate is bound to a route"),
    );
    await user.click(
      screen.getByRole("button", { name: "Delete example.com" }),
    );
    await user.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Delete certificate",
      }),
    );
    expect(
      await within(screen.getByRole("dialog")).findByRole("alert"),
    ).toHaveTextContent("certificate is bound to a route");
  });
  it("saves node account settings with a staging preset", async () => {
    const user = setup();
    await screen.findByText("No certificates yet");
    await user.click(screen.getByText(/Node ACME account settings/));
    await user.click(
      screen.getByRole("button", { name: "Let’s Encrypt staging" }),
    );
    await user.click(
      screen.getByRole("button", { name: "Save ACME settings" }),
    );
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "system/acme",
        {
          email: "admin@example.com",
          directory_url:
            "https://acme-staging-v02.api.letsencrypt.org/directory",
        },
        "PUT",
      ),
    );
  });
});
