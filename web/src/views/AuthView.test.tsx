import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { BootScreen } from "../components/InstanceScreen";
import { AuthView } from "./AuthView";

const state = vi.hoisted(() => ({
  force: false,
  setup: false,
  login: vi.fn(),
  logout: vi.fn(),
  updateAccount: vi.fn(),
}));
vi.mock("../providers/SessionProvider", () => ({
  useSession: () => ({
    ...state,
    user: state.force ? { must_change_password: true } : null,
    temporaryPassword: state.force ? "temporary-password" : "",
    setupRequired: state.setup,
  }),
}));
const view = () => (
  <MemoryRouter>
    <AuthView />
  </MemoryRouter>
);
beforeEach(() => {
  state.force = false;
  state.setup = false;
});
describe("authentication forms", () => {
  it("shows an accessible startup status without exposing sign-in fields", () => {
    render(<BootScreen />);
    expect(
      screen.getByRole("status", { name: "Loading Startship" }),
    ).toBeVisible();
    expect(screen.queryByLabelText("Username")).not.toBeInTheDocument();
  });
  it("shows the instance host and signs in with the supplied account", async () => {
    render(view());
    expect(
      screen.getByRole("heading", { name: "Sign in to Startship" }),
    ).toBeInTheDocument();
    expect(screen.getByText(window.location.host)).toBeInTheDocument();
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Username"), "admin");
    await user.type(screen.getByLabelText("Password"), "admin-password");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(state.login).toHaveBeenCalledWith("admin", "admin-password", false);
  });
  it("uses the same instance screen to set up the first administrator", () => {
    state.setup = true;
    render(view());
    expect(
      screen.getByRole("heading", { name: "Create the administrator" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toHaveAttribute(
      "autocomplete",
      "new-password",
    );
  });
  it("starts rotation with empty fields instead of reusing the sign-in credentials", async () => {
    const { rerender } = render(view());
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Username"), "admin");
    await user.type(screen.getByLabelText("Password"), "temporary-password");
    state.force = true;
    rerender(view());
    expect(screen.getByLabelText("New password")).toHaveValue("");
    expect(screen.getByLabelText("Confirm password")).toHaveValue("");
    await user.type(
      screen.getByLabelText("New password"),
      "replacement-password",
    );
    await user.type(
      screen.getByLabelText("Confirm password"),
      "mismatched-password",
    );
    await user.click(screen.getByRole("button", { name: "Set password" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Passwords do not match",
    );
    expect(state.updateAccount).not.toHaveBeenCalled();
  });
  it("confirms a matching replacement with the temporary credential", async () => {
    state.force = true;
    render(view());
    const user = userEvent.setup();
    await user.type(
      screen.getByLabelText("New password"),
      "replacement-password",
    );
    await user.type(
      screen.getByLabelText("Confirm password"),
      "replacement-password",
    );
    await user.click(screen.getByRole("button", { name: "Set password" }));
    expect(state.updateAccount).toHaveBeenCalledWith({
      current_password: "temporary-password",
      new_password: "replacement-password",
    });
  });
});
