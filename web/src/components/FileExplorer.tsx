import {
  lazy,
  Suspense,
  useEffect,
  useId,
  useRef,
  useState,
  type FormEvent,
  type MouseEvent,
} from "react";
import {
  ArrowDownToLine,
  Archive,
  ChevronRight,
  File,
  FilePlus,
  Folder,
  FolderPlus,
  HardDrive,
  Link2,
  MoreHorizontal,
  RefreshCw,
  Upload,
  X,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { request } from "../services/api";
import {
  childPath,
  fileEndpoint,
  isArchive,
  listFiles,
  type FileEntry,
} from "../services/files";
import { Popover } from "./Popover";
import { ErrorMessage } from "./ui";
import "./FileExplorer.css";

const FileEditor = lazy(() => import("./FileEditor"));
type Edit = { path: string; saved: string; value: string };
type Form = {
  title: string;
  value?: string;
  formats?: boolean;
  submit: (value: string, format: string) => Promise<unknown>;
};

function sizeLabel(size: number) {
  return size < 1024
    ? `${size} B`
    : size < 1048576
      ? `${(size / 1024).toFixed(1)} KB`
      : `${(size / 1048576).toFixed(1)} MB`;
}

function FileMenu({
  entry,
  href,
  reason,
  move,
  remove,
  extract,
}: {
  entry: FileEntry;
  href: (format?: string) => string;
  reason?: string;
  move?: () => void;
  remove?: () => void;
  extract?: () => void;
}) {
  const id = useId();
  return (
    <Popover
      id={id}
      label={`Actions for ${entry.name}`}
      trigger={<MoreHorizontal size={16} />}
      className="files-icon"
      menuClassName="files-menu"
    >
      {entry.type === "directory" ? (
        <>
          <a href={href("zip")} download>
            Download ZIP
          </a>
          <a href={href("tar.gz")} download>
            Download TAR.GZ
          </a>
        </>
      ) : (
        entry.type === "file" && (
          <a href={href()} download>
            Download
          </a>
        )
      )}
      {isArchive(entry.name) && entry.type === "file" && (
        <button
          type="button"
          disabled={!extract}
          title={!extract ? reason : undefined}
          onClick={extract}
        >
          Extract…
        </button>
      )}
      <button
        type="button"
        disabled={!move}
        title={!move ? reason : undefined}
        onClick={move}
      >
        Rename or move…
      </button>
      <button
        type="button"
        disabled={!remove}
        title={!remove ? reason : undefined}
        onClick={remove}
      >
        Delete…
      </button>
    </Popover>
  );
}

export default function FileExplorer({
  target,
  label,
  onClose,
}: {
  target: string;
  label: string;
  onClose: () => void;
}) {
  const title = useId();
  const formID = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const uploadRef = useRef<HTMLInputElement>(null);
  const [path, setPath] = useState("/");
  const [selection, setSelection] = useState<string[]>([]);
  const [edit, setEdit] = useState<Edit>();
  const [formatter, setFormatter] = useState<{
    run: (name: string, text: string) => Promise<string>;
  }>();
  const [form, setForm] = useState<Form>();
  const [format, setFormat] = useState("zip");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState<unknown>();
  const [dragging, setDragging] = useState(false);
  const listing = useQuery({
    queryKey: ["files", target, path],
    queryFn: ({ signal }) => listFiles(target, path, signal),
    refetchOnMount: "always",
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    retry: false,
  });
  const caps = listing.data?.capabilities;
  const reason = caps?.reason || "This action is not available on this target.";
  const dirty = edit !== undefined && edit.saved !== edit.value;
  const entries = listing.data?.entries || [];
  const selected = entries.filter((entry) => selection.includes(entry.name));
  const archive =
    selected.length === 1 &&
    selected[0].type === "file" &&
    isArchive(selected[0].name)
      ? selected[0]
      : undefined;
  const parts = path.split("/").filter(Boolean);
  const endpoint = (op: string, name: string, fmt?: string) =>
    fileEndpoint(target, op, name, fmt);
  const href = (name: string, fmt?: string) =>
    `/api/${endpoint("download", name, fmt)}`;

  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  useEffect(() => {
    if (!dirty) return;
    function preventLeave(event: BeforeUnloadEvent) {
      event.preventDefault();
    }
    window.addEventListener("beforeunload", preventLeave);
    return () => window.removeEventListener("beforeunload", preventLeave);
  }, [dirty]);

  function discard() {
    return !dirty || window.confirm("Discard unsaved changes?");
  }
  function close() {
    if (!busy && discard()) onClose();
  }
  function directory(next: string) {
    setPath(next);
    setSelection([]);
    setError(undefined);
    setForm(undefined);
  }
  async function run(
    activity: string,
    operation: () => Promise<unknown>,
    refresh = false,
  ) {
    if (busy) return;
    setBusy(activity);
    setError(undefined);
    try {
      await operation();
      if (refresh) {
        setSelection([]);
        await listing.refetch();
      }
    } catch (err) {
      setError(err);
    } finally {
      setBusy("");
    }
  }
  function open(entry: FileEntry) {
    const name = childPath(path, entry.name);
    if (entry.type === "directory") directory(name);
    if (entry.type === "file")
      void run("Opening…", async () => {
        const { text } = await request<{ text: string }>(
          endpoint("text", name),
        );
        const editor = await import("./FileEditor");
        setFormatter(
          editor.fileFormatter(name) ? { run: editor.formatFile } : undefined,
        );
        setEdit({ path: name, saved: text, value: text });
      });
  }
  function save() {
    if (!edit || !caps?.write || !dirty) return;
    void run("Saving…", async () => {
      await request(endpoint("save", edit.path), new Blob([edit.value]), "PUT");
      setEdit({ ...edit, saved: edit.value });
      await listing.refetch();
    });
  }
  useEffect(() => {
    if (!edit) return;
    function saveShortcut(event: globalThis.KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
        event.preventDefault();
        save();
      }
    }
    window.addEventListener("keydown", saveShortcut, true);
    return () => window.removeEventListener("keydown", saveShortcut, true);
  }, [edit, save]);
  function newEntry(type: "create" | "mkdir") {
    setForm({
      title: type === "create" ? "New file" : "New folder",
      value: "",
      submit: (name) =>
        request(endpoint(type, childPath(path, name)), undefined, "POST"),
    });
  }
  function compress() {
    setFormat("zip");
    setForm({
      title: "Compress selection",
      value: "archive",
      formats: true,
      submit: (name, format) =>
        request(endpoint("compress", childPath(path, `${name}.${format}`)), {
          paths: selected.map((entry) => childPath(path, entry.name)),
          format,
        }),
    });
  }
  function extract(entry: FileEntry) {
    setForm({
      title: `Extract ${entry.name}`,
      value: path,
      submit: (directory) =>
        request(endpoint("extract", childPath(path, entry.name)), {
          to: directory,
        }),
    });
  }
  function move(entry: FileEntry) {
    setForm({
      title: `Move ${entry.name}`,
      value: childPath(path, entry.name),
      submit: (to) =>
        request(endpoint("move", childPath(path, entry.name)), { to }),
    });
  }
  function remove(entry: FileEntry) {
    setForm({
      title: `Permanently delete ${entry.name}?`,
      submit: () =>
        request(
          endpoint("delete", childPath(path, entry.name)),
          undefined,
          "DELETE",
        ),
    });
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!form) return;
    const value = String(new FormData(event.currentTarget).get("target") || "");
    void run(
      "Working…",
      async () => {
        await form.submit(value, format);
        setForm(undefined);
      },
      true,
    );
  }
  function upload(files: FileList | null) {
    if (!files?.length || !caps?.write) return;
    const items = Array.from(files);
    void run(
      `Uploading ${items.length} ${items.length === 1 ? "file" : "files"}…`,
      async () => {
        try {
          for (const file of items)
            await request(
              endpoint("upload", childPath(path, file.name)),
              file,
              "PUT",
            );
        } finally {
          await listing.refetch();
        }
      },
    );
  }
  function contextMenu(event: MouseEvent<HTMLTableRowElement>) {
    const menu = event.currentTarget.querySelector<HTMLElement>("[popover]");
    if (menu?.showPopover) {
      event.preventDefault();
      menu.showPopover();
    }
  }

  return (
    <dialog
      ref={dialog}
      className="files-dialog"
      aria-labelledby={title}
      onCancel={(event) => {
        event.preventDefault();
        close();
      }}
    >
      <header className="files-head">
        <div className="files-title">
          <span className="files-mark">
            <HardDrive size={18} />
          </span>
          <div>
            <span className="eyebrow">FILE EXPLORER</span>
            <h2 id={title}>{label}</h2>
          </div>
        </div>
        <button
          type="button"
          className="files-icon"
          aria-label="Close explorer"
          disabled={!!busy}
          onClick={close}
        >
          <X size={18} />
        </button>
      </header>
      {edit ? (
        <>
          <div className="files-bar">
            <button
              type="button"
              disabled={!!busy}
              onClick={() => {
                if (discard()) {
                  setEdit(undefined);
                  setError(undefined);
                }
              }}
            >
              Back to files
            </button>
            <code className="files-edit-path">{edit.path}</code>
            <span className="files-spacer" />
            {dirty && <span className="small muted">Unsaved changes</span>}
            {!caps?.write && <span className="small muted">Read only</span>}
            {caps?.write && formatter && (
              <button
                type="button"
                disabled={!!busy}
                onClick={() =>
                  void run("Formatting…", async () => {
                    const value = await formatter.run(edit.path, edit.value);
                    setEdit({ ...edit, value });
                  })
                }
              >
                Format
              </button>
            )}
            {caps?.write && (
              <button
                type="button"
                className="primary"
                disabled={!!busy || !dirty}
                onClick={save}
              >
                Save
              </button>
            )}
          </div>
          <ErrorMessage error={error} />
          <Suspense fallback={<p className="files-empty">Loading editor…</p>}>
            <FileEditor
              name={edit.path}
              value={edit.value}
              readOnly={!caps?.write || !!busy}
              onChange={(value) =>
                setEdit((current) => current && { ...current, value })
              }
            />
          </Suspense>
        </>
      ) : (
        <>
          <div className="files-bar files-browse-bar">
            <nav className="files-crumbs" aria-label="Directory path">
              <button
                type="button"
                onClick={() => directory("/")}
                disabled={!!busy}
                aria-current={path === "/" ? "page" : undefined}
              >
                Root
              </button>
              {parts.map((part, index) => (
                <span key={index}>
                  <ChevronRight size={13} />
                  <button
                    type="button"
                    onClick={() =>
                      directory("/" + parts.slice(0, index + 1).join("/"))
                    }
                    disabled={!!busy}
                    aria-current={
                      index === parts.length - 1 ? "page" : undefined
                    }
                  >
                    {part}
                  </button>
                </span>
              ))}
            </nav>
            <span className="files-spacer" />
            <button
              type="button"
              disabled={!caps?.write || !!busy}
              onClick={() => newEntry("mkdir")}
            >
              <FolderPlus size={15} /> New folder
            </button>
            <button
              type="button"
              disabled={!caps?.write || !!busy}
              onClick={() => newEntry("create")}
            >
              <FilePlus size={15} /> New file
            </button>
            <button
              type="button"
              disabled={!caps?.write || !selected.length || !!busy}
              onClick={compress}
            >
              <Archive size={15} /> Compress
            </button>
            <button
              type="button"
              disabled={!caps?.write || !archive || !!busy}
              onClick={() => archive && extract(archive)}
            >
              Extract
            </button>
            <a
              href={href(path, "zip")}
              download
              className="button"
              title="Download current directory as ZIP"
            >
              <ArrowDownToLine size={15} /> Download
            </a>
            <button
              type="button"
              className="files-icon"
              disabled={!!busy}
              aria-label="Refresh files"
              onClick={() => void listing.refetch()}
            >
              <RefreshCw size={15} />
            </button>
            <input
              ref={uploadRef}
              hidden
              type="file"
              multiple
              onChange={(event) => {
                upload(event.currentTarget.files);
                event.currentTarget.value = "";
              }}
            />
            <button
              type="button"
              className="primary"
              disabled={!caps?.write || !!busy}
              onClick={() => uploadRef.current?.click()}
            >
              <Upload size={15} /> Upload
            </button>
          </div>
          {form && (
            <form className="files-form" onSubmit={submit}>
              <label htmlFor={formID}>{form.title}</label>
              {form.value !== undefined && (
                <input
                  autoFocus
                  required
                  id={formID}
                  name="target"
                  defaultValue={form.value}
                />
              )}
              {form.formats && (
                <div className="files-formats" aria-label="Archive format">
                  {["zip", "tar.gz"].map((value) => (
                    <button
                      key={value}
                      type="button"
                      aria-pressed={value === format}
                      onClick={() => setFormat(value)}
                    >
                      {value.toUpperCase()}
                    </button>
                  ))}
                </div>
              )}
              <button type="submit" className="primary" disabled={!!busy}>
                {form.value === undefined ? "Delete" : "Apply"}
              </button>
              <button
                type="button"
                disabled={!!busy}
                onClick={() => setForm(undefined)}
              >
                Cancel
              </button>
            </form>
          )}
          <ErrorMessage error={error || listing.error} />
          <div
            className={`files-table-wrap${dragging ? " files-dropping" : ""}`}
            onDragOver={(event) => {
              if (caps?.write) {
                event.preventDefault();
                setDragging(true);
              }
            }}
            onDragLeave={(event) => {
              if (!event.currentTarget.contains(event.relatedTarget as Node))
                setDragging(false);
            }}
            onDrop={(event) => {
              event.preventDefault();
              setDragging(false);
              upload(event.dataTransfer.files);
            }}
          >
            {dragging && (
              <div className="files-drop-label">
                <Upload size={22} /> Drop files to upload
              </div>
            )}
            <table className="files-table">
              <thead>
                <tr>
                  <th scope="col" className="files-select">
                    <input
                      type="checkbox"
                      aria-label="Select all files"
                      checked={
                        !!entries.length && selected.length === entries.length
                      }
                      onChange={(event) =>
                        setSelection(
                          event.target.checked
                            ? entries.map((entry) => entry.name)
                            : [],
                        )
                      }
                    />
                  </th>
                  <th scope="col">Name</th>
                  <th scope="col">Size</th>
                  <th scope="col">Mode</th>
                  <th scope="col">Modified</th>
                  <th scope="col">
                    <span className="files-visually-hidden">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => (
                  <tr
                    key={entry.name}
                    onContextMenu={contextMenu}
                    aria-selected={selection.includes(entry.name)}
                  >
                    <td>
                      <input
                        type="checkbox"
                        aria-label={`Select ${entry.name}`}
                        checked={selection.includes(entry.name)}
                        onChange={(event) =>
                          setSelection(
                            event.target.checked
                              ? [...selection, entry.name]
                              : selection.filter((item) => item !== entry.name),
                          )
                        }
                      />
                    </td>
                    <td>
                      {entry.type === "directory" || entry.type === "file" ? (
                        <button
                          type="button"
                          className="files-name"
                          disabled={!!busy}
                          onClick={() => open(entry)}
                        >
                          {entry.type === "directory" ? (
                            <Folder size={17} />
                          ) : isArchive(entry.name) ? (
                            <Archive size={17} />
                          ) : (
                            <File size={17} />
                          )}
                          {entry.name}
                        </button>
                      ) : (
                        <span className="files-name" title={entry.type}>
                          <Link2 size={17} />
                          {entry.name}
                          {entry.link && <small> → {entry.link}</small>}
                        </span>
                      )}
                    </td>
                    <td>
                      {entry.type === "file" ? sizeLabel(entry.size) : "—"}
                    </td>
                    <td>
                      <code>{entry.mode.toString(8).padStart(4, "0")}</code>
                    </td>
                    <td>
                      <time dateTime={entry.mtime}>
                        {new Date(entry.mtime).toLocaleString()}
                      </time>
                    </td>
                    <td>
                      <FileMenu
                        entry={entry}
                        href={(format) =>
                          href(childPath(path, entry.name), format)
                        }
                        reason={reason}
                        move={
                          caps?.move && !busy ? () => move(entry) : undefined
                        }
                        remove={
                          caps?.delete && !busy
                            ? () => remove(entry)
                            : undefined
                        }
                        extract={
                          caps?.write && !busy
                            ? () => extract(entry)
                            : undefined
                        }
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!entries.length && (
              <div className="files-empty">
                <Folder size={22} />
                <strong>
                  {listing.isPending
                    ? "Loading files…"
                    : listing.error
                      ? "Files unavailable"
                      : "This folder is empty"}
                </strong>
                {caps?.write && <small>Drop files here or use Upload.</small>}
              </div>
            )}
          </div>
          <footer className="files-footer">
            <span>
              {entries.length} {entries.length === 1 ? "item" : "items"}
              {selected.length ? ` · ${selected.length} selected` : ""}
            </span>
            <span>{caps?.reason}</span>
          </footer>
        </>
      )}
      {busy && (
        <div className="files-progress" role="status">
          {busy}
        </div>
      )}
    </dialog>
  );
}
