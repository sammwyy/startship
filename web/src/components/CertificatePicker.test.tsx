import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  afterAll,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import { CertificatePicker } from "./CertificatePicker";
import { request } from "../services/api";
import type { Certificate } from "../services/types";

vi.mock("../services/api", () => ({ request: vi.fn() }));

const methods = ["showPopover", "hidePopover", "scrollIntoView"] as const;
const originals = methods.map((name) =>
  Object.getOwnPropertyDescriptor(HTMLElement.prototype, name),
);
beforeAll(() =>
  methods.forEach((name) =>
    Object.defineProperty(HTMLElement.prototype, name, {
      configurable: true,
      value: vi.fn(),
    }),
  ),
);
afterAll(() =>
  methods.forEach((name, index) => {
    if (originals[index])
      Object.defineProperty(HTMLElement.prototype, name, originals[index]!);
    else
      delete (HTMLElement.prototype as unknown as Record<string, unknown>)[
        name
      ];
  }),
);
beforeEach(() => {
  vi.mocked(request).mockReset();
});

const certificate: Certificate = {
  id: "new",
  workspace_id: "w",
  domains: ["app.example.com"],
  source: "acme",
  issuer: "",
  not_before: "",
  not_after: "",
  status: "pending",
  auto_renew: true,
};
const defaults = {
  host: "app.example.com",
  domains: [
    { id: "d", name: "example.com", apex: true, subdomains: true, routes: 0 },
  ],
  workspaceId: "w",
  certificates: [],
  available: true,
  value: "",
  onChange: vi.fn(),
};

describe("route certificate dropdown", () => {
  it("navigates options with arrows, Enter and Escape and restores trigger focus", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <CertificatePicker
        {...defaults}
        onChange={onChange}
        certificates={[{ ...certificate, source: "upload", status: "active" }]}
      />,
    );
    const trigger = screen.getByRole("button", { name: "Certificate" });
    expect(trigger).toHaveAttribute("aria-haspopup", "listbox");
    trigger.focus();
    await user.keyboard("{ArrowDown}");
    expect(screen.getByRole("option", { name: /Automatic/ })).toHaveFocus();
    await user.keyboard("{ArrowDown}{Enter}");
    expect(onChange).toHaveBeenCalledWith("new");
    expect(trigger).toHaveFocus();
    await user.keyboard("{ArrowUp}");
    expect(screen.getByRole("option", { name: /Generate/ })).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(trigger).toHaveFocus();
    expect(HTMLElement.prototype.hidePopover).toHaveBeenCalled();
  });
  it("generates and selects a certificate, then follows live active and failure updates", async () => {
    const user = userEvent.setup(),
      onChange = vi.fn();
    vi.mocked(request).mockResolvedValue(certificate);
    const view = render(
      <CertificatePicker {...defaults} onChange={onChange} />,
    );
    await user.click(screen.getByRole("option", { name: /Generate/ }));
    expect(request).toHaveBeenCalledWith("workspaces/w/certificates", {
      source: "acme",
      domains: ["app.example.com"],
      auto_renew: true,
    });
    expect(onChange).toHaveBeenCalledWith("new");
    view.rerender(<CertificatePicker {...defaults} value="new" />);
    expect(screen.getByRole("status")).toHaveTextContent("Issuing");
    view.rerender(
      <CertificatePicker
        {...defaults}
        value="new"
        certificates={[
          {
            ...certificate,
            status: "active",
            not_after: "2099-01-01T00:00:00Z",
          },
        ]}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("Active");
    expect(screen.getByRole("status")).toHaveTextContent("days left");
    view.rerender(
      <CertificatePicker
        {...defaults}
        value="new"
        certificates={[
          {
            ...certificate,
            status: "failed",
            last_error: "DNS validation failed",
          },
        ]}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "DNS validation failed",
    );
  });
  it("shows API errors and lets the user retry", async () => {
    vi.mocked(request).mockRejectedValue(
      new Error("Configure ACME account email first"),
    );
    const user = userEvent.setup();
    render(<CertificatePicker {...defaults} />);
    await user.click(screen.getByRole("option", { name: /Generate/ }));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Configure ACME account email first",
    );
    expect(screen.getByRole("option", { name: /Generate/ })).toBeEnabled();
  });
  it.each([
    [{ host: "" }, /Enter a hostname/],
    [{ host: "*.example.com" }, /cannot issue wildcard/],
    [{ host: "[subdomain].example.com" }, /valid domain hostname/],
    [{ domains: [] }, /not covered by workspace domain claims/],
    [{ available: false }, /Requires workspace.domains/],
    [{ certificates: [certificate] }, /already covers/],
    [
      { certificates: [{ ...certificate, status: "issuing" as const }] },
      /already covers/,
    ],
    [
      { certificates: [{ ...certificate, status: "active" as const }] },
      /already covers/,
    ],
  ])("disables unavailable generation with a reason (%j)", (props, reason) => {
    render(<CertificatePicker {...defaults} {...props} />);
    expect(screen.getByRole("option", { name: /Generate/ })).toBeDisabled();
    expect(screen.getByRole("option", { name: /Generate/ })).toHaveTextContent(
      reason,
    );
  });
  it("allows admins outside claims and ordinary users on nested claimed subdomains", () => {
    const view = render(<CertificatePicker {...defaults} domains={[]} admin />);
    expect(screen.getByRole("option", { name: /Generate/ })).toBeEnabled();
    view.rerender(
      <CertificatePicker {...defaults} host="deep.app.example.com" />,
    );
    expect(screen.getByRole("option", { name: /Generate/ })).toBeEnabled();
  });
  it("filters certificates by coverage and can switch back to Automatic", async () => {
    const onChange = vi.fn(),
      user = userEvent.setup();
    render(
      <CertificatePicker
        {...defaults}
        value="new"
        onChange={onChange}
        certificates={[
          certificate,
          { ...certificate, id: "other", domains: ["other.test"] },
        ]}
      />,
    );
    expect(
      screen.queryByRole("option", { name: /other.test/ }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("option", { name: /Automatic/ }));
    expect(onChange).toHaveBeenCalledWith("");
  });
});
