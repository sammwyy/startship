import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LogPanel } from "./LogPanel";
vi.mock("../providers/UIProvider", () => ({
  useUI: () => ({ run: vi.fn(), notify: vi.fn() }),
}));
afterEach(cleanup);
describe("log console", () => {
  it("shows a waiting state instead of suggesting another deploy for active builds", () => {
    render(<LogPanel status="building" />);
    expect(screen.getByText("Waiting for build output…")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy logs" })).toHaveProperty(
      "disabled",
      true,
    );
  });
  it("keeps exact output, supports horizontal scrolling and shows line count", () => {
    render(<LogPanel log={"one\ntwo\n"} status="running" />);
    expect(screen.getByLabelText("Project logs").textContent).toBe(
      "one\ntwo\n",
    );
    expect(screen.getByText("2 lines")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Wrap" }));
    expect(
      screen.getByLabelText("Project logs").classList.contains("log-no-wrap"),
    ).toBe(true);
  });
});
