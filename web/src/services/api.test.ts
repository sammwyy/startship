import { afterEach, describe, expect, it, vi } from "vitest";
import { beginSessionChange, request, setCredentialRotation } from "./api";
afterEach(() => {
  vi.unstubAllGlobals();
  setCredentialRotation(false);
});
function response(status: number, body: unknown) {
  return { ok: status < 400, status, json: async () => body } as Response;
}
describe("API service", () => {
  it("uses same-origin sessions, JSON mutations and API error messages", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(response(403, { error: "not permitted" }));
    vi.stubGlobal("fetch", fetch);
    await expect(request("projects", { name: "test" })).rejects.toThrow(
      "not permitted",
    );
    expect(fetch).toHaveBeenCalledWith(
      "/api/projects",
      expect.objectContaining({
        method: "POST",
        credentials: "same-origin",
        body: '{"name":"test"}',
      }),
    );
  });
  it("ignores stale polling authorization errors during credential rotation", async () => {
    let resolve!: (response: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn(
        () =>
          new Promise<Response>((done) => {
            resolve = done;
          }),
      ),
    );
    const expired = vi.fn();
    window.addEventListener("session-expired", expired);
    try {
      const pending = request("releases");
      beginSessionChange();
      resolve(response(401, { error: "expired" }));
      await expect(pending).rejects.toThrow("expired");
      expect(expired).not.toHaveBeenCalled();
      setCredentialRotation(true);
      const duringSave = request("releases");
      resolve(response(401, {}));
      await expect(duringSave).rejects.toThrow();
      expect(expired).not.toHaveBeenCalled();
      setCredentialRotation(false);
      const signedOut = request("me");
      resolve(response(401, {}));
      await expect(signedOut).rejects.toThrow();
      expect(expired).toHaveBeenCalledOnce();
    } finally {
      window.removeEventListener("session-expired", expired);
    }
  });
  it("passes cancellation through to fetch", async () => {
    const fetch = vi.fn().mockResolvedValue(response(200, []));
    vi.stubGlobal("fetch", fetch);
    const controller = new AbortController();
    await request("workspaces", undefined, "GET", controller.signal);
    expect(fetch.mock.calls[0][1].signal).toBe(controller.signal);
  });
});
