import { useEffect, useId, useRef, useState } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { request } from "../services/api";
import { Icon } from "./Icon";
import "./TerminalDialog.css";

export default function TerminalDialog({
  target,
  label,
  onClose,
}: {
  target: string;
  label: string;
  onClose: () => void;
}) {
  const id = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const surface = useRef<HTMLDivElement>(null);
  const [attempt, setAttempt] = useState(0);
  const [notice, setNotice] = useState("Connecting…");
  const [ended, setEnded] = useState(false);

  useEffect(() => {
    const el = dialog.current!;
    el.showModal();
    return () => el.close();
  }, []);

  useEffect(() => {
    const host = surface.current!;
    const abort = new AbortController();
    let socket: WebSocket | undefined;
    let exitCode: number | undefined;
    const terminal = new Terminal({
      cursorBlink: true,
      fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace",
      fontSize: 13,
      scrollback: 3000,
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    const updateTheme = () => {
      const style = getComputedStyle(host);
      terminal.options.theme = {
        background: style.backgroundColor,
        foreground: style.color,
        cursor: style.color,
      };
    };
    updateTheme();
    terminal.open(host);
    fit.fit();
    terminal.focus();

    const themes = new MutationObserver(updateTheme);
    themes.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme", "data-theme-style", "data-accent"],
    });
    const sizes = new ResizeObserver(() => fit.fit());
    sizes.observe(host);
    const sendSize = (type: "start" | "resize") => {
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send(
          JSON.stringify({ type, rows: terminal.rows, cols: terminal.cols }),
        );
      }
    };
    const input = terminal.onData((data) => {
      if (socket?.readyState === WebSocket.OPEN) {
        const bytes = new TextEncoder().encode(data);
        for (let i = 0; i < bytes.length; i += 16384)
          socket.send(bytes.subarray(i, i + 16384));
      }
    });
    const resize = terminal.onResize(() => sendSize("resize"));
    const end = (message: string) => {
      setNotice(message);
      setEnded(true);
    };

    const endpoint = `${target}/exec`;
    void request(endpoint, undefined, "GET", abort.signal)
      .then(() => {
        if (abort.signal.aborted) return;
        const url = new URL(`/api/${endpoint}`, window.location.href);
        url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
        socket = new WebSocket(url);
        socket.binaryType = "arraybuffer";
        socket.onopen = () => {
          sendSize("start");
          setNotice("");
        };
        socket.onmessage = ({ data }) => {
          if (data instanceof ArrayBuffer) {
            terminal.write(new Uint8Array(data));
          } else if (typeof data === "string") {
            const event = JSON.parse(data);
            if (event.type === "exit" && typeof event.code === "number")
              exitCode = event.code;
          }
        };
        socket.onclose = ({ reason }) =>
          end(
            exitCode === undefined
              ? reason || "Terminal disconnected."
              : `Process exited (code ${exitCode}).`,
          );
      })
      .catch((error: unknown) => {
        if (!abort.signal.aborted)
          end(error instanceof Error ? error.message : "Could not connect.");
      });
    return () => {
      abort.abort();
      if (socket) {
        socket.onopen = socket.onmessage = socket.onclose = null;
        socket.close();
      }
      themes.disconnect();
      sizes.disconnect();
      input.dispose();
      resize.dispose();
      terminal.dispose();
    };
  }, [target, attempt]);

  return (
    <dialog
      ref={dialog}
      className="container-terminal"
      aria-labelledby={id}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
    >
      <header className="container-terminal-header">
        <div>
          <h2 id={id}>Terminal</h2>
          <span className="small muted">{label}</span>
        </div>
        <button
          type="button"
          className="icon-button"
          aria-label="Close terminal"
          onClick={onClose}
        >
          <Icon name="close" />
        </button>
      </header>
      <div ref={surface} className="container-terminal-surface" />
      <footer className="container-terminal-footer">
        <span className="small muted" role="status">
          {notice}
        </span>
        {ended && (
          <button
            type="button"
            onClick={() => {
              setNotice("Connecting…");
              setEnded(false);
              setAttempt((value) => value + 1);
            }}
          >
            Reconnect
          </button>
        )}
      </footer>
    </dialog>
  );
}
