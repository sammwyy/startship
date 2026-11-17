import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { AsyncForm, Field, ThemeButton } from "./ui";
import { HostPicker, pickedHost } from "./HostPicker";

describe("shared components", () => {
  it("disables a pending form, prevents duplicate requests and reports failure", async () => {
    let reject!: (error: Error) => void;
    const submit = vi.fn(
      () =>
        new Promise<void>((_, fail) => {
          reject = fail;
        }),
    );
    render(
      <AsyncForm onSubmit={submit}>
        <Field label="Name" name="name" defaultValue="project" />
        <button>Save</button>
      </AsyncForm>,
    );
    const user = userEvent.setup();
    await user.click(screen.getByText("Save"));
    expect(screen.getByText("Save")).toBeDisabled();
    await user.click(screen.getByText("Save"));
    expect(submit).toHaveBeenCalledOnce();
    reject(new Error("Save failed"));
    await screen.findByRole("alert");
    expect(screen.getByRole("alert")).toHaveTextContent("Save failed");
    expect(screen.getByText("Save")).toBeEnabled();
  });
  it("does not submit read-only forms", async () => {
    const submit = vi.fn();
    render(
      <AsyncForm disabled onSubmit={submit}>
        <Field label="Name" name="name" />
        <button>Save</button>
      </AsyncForm>,
    );
    await userEvent.click(screen.getByText("Save"));
    expect(submit).not.toHaveBeenCalled();
    expect(screen.getByLabelText("Name")).toBeDisabled();
  });
  it("persists the light and dark toggle", async () => {
    document.documentElement.dataset.theme = "dark";
    render(<ThemeButton />);
    await userEvent.click(
      screen.getByRole("button", { name: "Switch to light theme" }),
    );
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(localStorage.getItem("startship-theme")).toBe("light");
  });
  it("composes constrained exact, wildcard and administrator hostnames", async () => {
    render(
      <HostPicker
        admin
        domains={[
          {
            id: "d",
            name: "example.com",
            apex: true,
            subdomains: true,
            routes: 0,
          },
        ]}
        host="api.example.com"
      />,
    );
    expect(screen.getByRole("textbox", { name: "Subdomain" })).toHaveValue(
      "api",
    );
    const form = new FormData();
    form.set("domain", "*.example.com");
    form.set("sub", " api. ");
    expect(pickedHost(form)).toBe("api.example.com");
    form.set("domain", "example.com");
    expect(pickedHost(form)).toBe("example.com");
    await userEvent.selectOptions(screen.getByLabelText("Domain"), "");
    await waitFor(() =>
      expect(screen.getByRole("textbox", { name: "Hostname" })).toBeRequired(),
    );
  });
});
