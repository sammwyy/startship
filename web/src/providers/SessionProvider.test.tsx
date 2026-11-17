import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { request } from "../services/api";
import { SessionProvider, useSession } from "./SessionProvider";

vi.mock("../services/api", async (original) => ({
  ...(await original<typeof import("../services/api")>()),
  request: vi.fn(),
}));
let reads = 0;
let failRefresh = false;
const lostAccess = vi.fn();
function Probe() {
  const session = useSession();
  const [done, setDone] = useState(false);
  if (
    session.user &&
    (!session.user.platform_admin || !session.can("users.manage"))
  )
    lostAccess();
  return (
    <>
      <span>{session.user?.username}</span>
      {session.can("users.manage") && <span>Admin controls</span>}
      {session.user?.must_change_password && <span>Rotation required</span>}
      <button
        onClick={() =>
          void session
            .updateAccount({
              current_password: "temporary",
              new_password: "replacement",
              username: "renamed",
            })
            .then(() => setDone(true))
        }
      >
        Save account
      </button>
      {done && <span>Saved</span>}
    </>
  );
}
beforeEach(() => {
  reads = 0;
  failRefresh = false;
  vi.mocked(request).mockImplementation(async (path, body) => {
    if (path === "setup") return { required: false };
    if (body) return { id: "admin", username: "renamed" };
    reads++;
    if (reads > 1 && failRefresh)
      throw new Error("Daemon temporarily unavailable");
    return {
      id: "admin",
      username: reads > 1 ? "renamed" : "admin",
      permissions: ["users.manage"],
      platform_admin: true,
      must_change_password: reads === 1,
    };
  });
});
describe("account session updates", () => {
  it.each([false, true])(
    "preserves privileges after PATCH /me, including when refresh fails: %s",
    async (unavailable) => {
      failRefresh = unavailable;
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false, gcTime: 0 } },
      });
      render(
        <QueryClientProvider client={client}>
          <SessionProvider>
            <Probe />
          </SessionProvider>
        </QueryClientProvider>,
      );
      await screen.findByText("Admin controls");
      expect(screen.getByText("Rotation required")).toBeInTheDocument();
      await userEvent.click(screen.getByText("Save account"));
      await screen.findByText("Saved");
      expect(screen.getByText("renamed")).toBeInTheDocument();
      expect(screen.getByText("Admin controls")).toBeInTheDocument();
      expect(screen.queryByText("Rotation required")).not.toBeInTheDocument();
      expect(lostAccess).not.toHaveBeenCalled();
      expect(reads).toBe(2);
    },
  );
});
