import { useEffect, useRef } from "react";
import { EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { StreamLanguage } from "@codemirror/language";
import { json } from "@codemirror/lang-json";
import { yaml } from "@codemirror/lang-yaml";
import { javascript } from "@codemirror/lang-javascript";
import { css } from "@codemirror/lang-css";
import { html } from "@codemirror/lang-html";
import { markdown } from "@codemirror/lang-markdown";
import { xml } from "@codemirror/lang-xml";
import { sql } from "@codemirror/lang-sql";
import { dockerFile } from "@codemirror/legacy-modes/mode/dockerfile";
import { nginx } from "@codemirror/legacy-modes/mode/nginx";
import { properties } from "@codemirror/legacy-modes/mode/properties";
import { shell } from "@codemirror/legacy-modes/mode/shell";
import { toml } from "@codemirror/legacy-modes/mode/toml";
import { oneDark } from "@codemirror/theme-one-dark";
import { basicSetup } from "codemirror";
import { useAppliedTheme } from "../lib/theme";

export function fileLanguage(name: string) {
  const n = (name.split("/").pop() || "").toLowerCase();
  if (/^dockerfile([.-]|$)/.test(n)) return "dockerfile";
  if (/^\.env([.-]|$)/.test(n)) return "env";
  const ext = n.split(".").pop() || "";
  return { yml: "yaml", conf: "nginx", bash: "sh", zsh: "sh" }[ext] || ext;
}

function syntax(name: string) {
  switch (fileLanguage(name)) {
    case "json":
      return json();
    case "yaml":
      return yaml();
    case "js":
    case "mjs":
    case "cjs":
      return javascript();
    case "jsx":
      return javascript({ jsx: true });
    case "ts":
      return javascript({ typescript: true });
    case "tsx":
      return javascript({ typescript: true, jsx: true });
    case "css":
      return css();
    case "html":
      return html();
    case "md":
      return markdown();
    case "xml":
      return xml();
    case "sql":
      return sql();
    case "dockerfile":
      return StreamLanguage.define(dockerFile);
    case "nginx":
      return StreamLanguage.define(nginx);
    case "env":
    case "ini":
      return StreamLanguage.define(properties);
    case "sh":
      return StreamLanguage.define(shell);
    case "toml":
      return StreamLanguage.define(toml);
    default:
      return [];
  }
}

export function fileFormatter(name: string) {
  const language = fileLanguage(name);
  switch (language) {
    case "json":
    case "js":
    case "jsx":
    case "mjs":
    case "cjs":
      return {
        parser: language === "json" ? "json" : "babel",
        plugins: () =>
          Promise.all([
            import("prettier/plugins/babel"),
            import("prettier/plugins/estree"),
          ]),
      };
    case "ts":
    case "tsx":
      return {
        parser: "typescript",
        plugins: () =>
          Promise.all([
            import("prettier/plugins/typescript"),
            import("prettier/plugins/estree"),
          ]),
      };
    case "yaml":
      return {
        parser: "yaml",
        plugins: () => Promise.all([import("prettier/plugins/yaml")]),
      };
    case "css":
      return {
        parser: "css",
        plugins: () => Promise.all([import("prettier/plugins/postcss")]),
      };
    case "html":
      return {
        parser: "html",
        plugins: () => Promise.all([import("prettier/plugins/html")]),
      };
    case "md":
      return {
        parser: "markdown",
        plugins: () => Promise.all([import("prettier/plugins/markdown")]),
      };
    default:
      return undefined;
  }
}

export async function formatFile(name: string, value: string) {
  const config = fileFormatter(name);
  if (!config) return value;
  const [prettier, plugins] = await Promise.all([
    import("prettier/standalone"),
    config.plugins(),
  ]);
  return prettier.format(value, {
    parser: config.parser,
    plugins: plugins.map((module) => module.default),
  });
}

export default function FileEditor({
  name,
  value,
  readOnly,
  onChange,
}: {
  name: string;
  value: string;
  readOnly: boolean;
  onChange: (value: string) => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView>(null);
  const theme = useAppliedTheme();
  const current = useRef({ value, onChange });
  useEffect(() => {
    current.current = { value, onChange };
    const editor = view.current;
    if (editor && editor.state.doc.toString() !== value) {
      editor.dispatch({
        changes: { from: 0, to: editor.state.doc.length, insert: value },
      });
    }
  }, [value, onChange]);
  useEffect(() => {
    const editor = new EditorView({
      parent: root.current!,
      state: EditorState.create({
        doc: current.current.value,
        extensions: [
          basicSetup,
          syntax(name),
          theme === "dark" ? oneDark : [],
          EditorState.readOnly.of(readOnly),
          EditorView.editable.of(!readOnly),
          EditorView.contentAttributes.of({
            "aria-label": name,
            spellcheck: "false",
          }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged)
              current.current.onChange(update.state.doc.toString());
          }),
        ],
      }),
    });
    view.current = editor;
    return () => {
      view.current = null;
      editor.destroy();
    };
  }, [name, theme, readOnly]);
  return <div className="files-code" ref={root} />;
}
