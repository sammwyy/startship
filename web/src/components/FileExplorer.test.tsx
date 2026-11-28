import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import FileExplorer from "./FileExplorer";
import { fileLanguage, formatFile } from "./FileEditor";
import { fileEndpoint } from "../services/files";

afterEach(() => vi.unstubAllGlobals());

describe("file explorer", () => {
  it("encodes paths as query parameters", () => {
    expect(fileEndpoint("volumes/test", "download", "/dir/a?b#c")).toBe(
      "volumes/test/files/download?path=%2Fdir%2Fa%3Fb%23c",
    );
  });

  it("can browse a read-only target without exposing writes", async () => {
    const fetch = vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify({
            entries: url.endsWith("%2F")
              ? [
                  {
                    name: "config",
                    type: "directory",
                    mode: 493,
                    size: 0,
                    mtime: "2026-01-01T00:00:00Z",
                  },
                ]
              : [],
            capabilities: {
              write: false,
              move: false,
              delete: false,
              reason: "You have read-only file access.",
            },
          }),
        ),
    );
    vi.stubGlobal("fetch", fetch);
    const show = HTMLDialogElement.prototype.showModal;
    HTMLDialogElement.prototype.showModal = function () {
      this.setAttribute("open", "");
    };
    try {
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      render(
        <QueryClientProvider client={client}>
          <FileExplorer target="volumes/test" label="Data" onClose={() => {}} />
        </QueryClientProvider>,
      );
      const config = await screen.findByRole("button", { name: "config" });
      expect(screen.getByRole("button", { name: "Upload" })).toBeDisabled();
      fireEvent.click(config);
      expect(
        await screen.findByText("This folder is empty"),
      ).toBeInTheDocument();
      expect(fetch).toHaveBeenCalledWith(
        "/api/volumes/test/files/list?path=%2Fconfig",
        expect.anything(),
      );
    } finally {
      HTMLDialogElement.prototype.showModal = show;
    }
  });

  it("saves a formatted edit from the keyboard even when focus is outside the editor", async () => {
    const fetch = vi.fn(async (url: string) => {
      const body = url.includes("/text?")
        ? { text: '{"port":8080}' }
        : url.includes("/save?")
          ? { ok: true }
          : {
              entries: [
                {
                  name: "config.json",
                  type: "file",
                  mode: 420,
                  size: 13,
                  mtime: "2026-01-01T00:00:00Z",
                },
              ],
              capabilities: { write: true, move: true, delete: true },
            };
      return Response.json(body);
    });
    vi.stubGlobal("fetch", fetch);
    const show = HTMLDialogElement.prototype.showModal;
    HTMLDialogElement.prototype.showModal = function () {
      this.setAttribute("open", "");
    };
    try {
      render(
        <QueryClientProvider client={new QueryClient()}>
          <FileExplorer target="volumes/test" label="Data" onClose={() => {}} />
        </QueryClientProvider>,
      );
      fireEvent.click(
        await screen.findByRole("button", { name: "config.json" }),
      );
      fireEvent.click(await screen.findByRole("button", { name: "Format" }));
      await screen.findByText("Unsaved changes");
      expect(
        fireEvent.keyDown(document.body, { key: "s", ctrlKey: true }),
      ).toBe(false);
      expect(fetch).toHaveBeenCalledWith(
        "/api/volumes/test/files/save?path=%2Fconfig.json",
        expect.objectContaining({ method: "PUT", body: expect.any(Blob) }),
      );
    } finally {
      HTMLDialogElement.prototype.showModal = show;
    }
  });
});

describe("file editor", () => {
  it("detects config formats and formats JSON and YAML", async () => {
    expect(fileLanguage("/Dockerfile.worker")).toBe("dockerfile");
    expect(fileLanguage("/.env.local")).toBe("env");
    expect(fileLanguage("/nginx.conf")).toBe("nginx");
    expect(await formatFile("config.json", '{"port":8080}')).toBe(
      '{ "port": 8080 }\n',
    );
    expect(await formatFile("config.yml", "port:    8080")).toBe(
      "port: 8080\n",
    );
  });
});
