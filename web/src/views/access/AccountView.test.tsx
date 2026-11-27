import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import index from "../../../index.html?raw";
import { ThemeButton } from "../../components/ui";
import { UIProvider } from "../../providers/UIProvider";
import { AccountView } from "./AccountView";

const root = document.documentElement;

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
  delete root.dataset.theme;
  delete root.dataset.themePreference;
  delete root.dataset.themeStyle;
  delete root.dataset.accent;
});

function renderAppearance() {
  render(
    <MemoryRouter initialEntries={["/account/appearance"]}>
      <UIProvider>
        <Routes>
          <Route
            path="/account/:section"
            element={
              <>
                <ThemeButton />
                <AccountView />
              </>
            }
          />
        </Routes>
      </UIProvider>
    </MemoryRouter>,
  );
}

function option(group: string, name: RegExp) {
  return within(screen.getByRole("group", { name: group })).getByRole("radio", {
    name,
  });
}

function runPrePaint(pathname = "/", light = false) {
  const script = new DOMParser()
    .parseFromString(index, "text/html")
    .querySelector("head script")!.textContent!;
  new Function("location", "matchMedia", script)({ pathname }, () => ({
    matches: light,
  }));
}

it("defaults to System, Tinted, and Green when preferences are empty", () => {
  runPrePaint("/", true);
  renderAppearance();
  expect(root.dataset.theme).toBe("light");
  expect(option("Mode", /System/)).toBeChecked();
  expect(option("Theme", /Tinted/)).toBeChecked();
  expect(option("Accent", /Green/)).toBeChecked();
});

it("persists style, accent and mode independently and restores them before paint", async () => {
  runPrePaint();
  renderAppearance();
  const user = userEvent.setup();
  await user.click(option("Theme", /Carbon/));
  await user.click(option("Accent", /Blue/));
  await user.click(
    screen.getByRole("button", { name: "Switch to light theme" }),
  );
  expect(option("Mode", /Light/)).toBeChecked();
  expect(option("Theme", /Carbon/)).toBeChecked();
  expect(option("Accent", /Blue/)).toBeChecked();
  expect(root.dataset.themeStyle).toBe("carbon");
  expect(root.dataset.accent).toBe("blue");
  expect(localStorage.getItem("startship-theme")).toBe("light");
  expect(localStorage.getItem("startship-theme-style")).toBe("carbon");
  expect(localStorage.getItem("startship-theme-accent")).toBe("blue");

  delete root.dataset.theme;
  delete root.dataset.themeStyle;
  delete root.dataset.accent;
  runPrePaint();
  expect(root.dataset.theme).toBe("light");
  expect(root.dataset.themeStyle).toBe("carbon");
  expect(root.dataset.accent).toBe("blue");
});

it("uses separate public status preferences and ignores invalid saved values", () => {
  localStorage.setItem("startship-theme", "light");
  localStorage.setItem("startship-theme-style", "carbon");
  localStorage.setItem("startship-theme-accent", "blue");
  localStorage.setItem("startship-status-theme", "dark");
  localStorage.setItem("startship-status-theme-style", "invalid");
  localStorage.setItem("startship-status-theme-accent", "violet");
  runPrePaint("/status/demo", true);
  expect(root.dataset.theme).toBe("dark");
  expect(root.dataset.themeStyle).toBe("tinted");
  expect(root.dataset.accent).toBe("violet");
});

it("still applies appearance changes when browser storage is blocked", async () => {
  const deny = () => {
    throw new DOMException("Storage blocked", "SecurityError");
  };
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(deny);
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(deny);
  runPrePaint();
  expect(root.dataset.themeStyle).toBe("tinted");
  expect(root.dataset.accent).toBe("green");
  renderAppearance();
  const user = userEvent.setup();
  await user.click(option("Theme", /Carbon/));
  await user.click(option("Accent", /Pink/));
  expect(root.dataset.themeStyle).toBe("carbon");
  expect(root.dataset.accent).toBe("pink");
});
