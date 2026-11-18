import { useEffect, useRef, useState } from "react";
import type { PipelineTemplate } from "../services/types";
import { Icon } from "./Icon";
import { TemplateIcon } from "./TemplateIcon";

export function PipelineTemplateModal({
  templates,
  current,
  detected,
  onSelect,
  onClose,
}: {
  templates: PipelineTemplate[];
  current: string;
  detected: string[];
  onSelect: (template: PipelineTemplate) => void;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [search, setSearch] = useState(""),
    [category, setCategory] = useState("All");
  useEffect(() => {
    const el = dialog.current!;
    if (!el.open) el.showModal();
  }, []);
  const filtered = templates.filter(
    (t) =>
      (category === "All" || t.category === category) &&
      `${t.name} ${t.description}`.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <dialog
      ref={dialog}
      className="pipeline-modal"
      aria-labelledby="pipeline-modal-title"
      onCancel={onClose}
      onClose={onClose}
    >
      <div className="pipeline-modal-head">
        <div>
          <span className="pipeline-eyebrow">PIPELINE LIBRARY</span>
          <h2 id="pipeline-modal-title">Choose your starting point</h2>
          <p className="small muted">
            Ready-to-use stages. Make them your own before deploying.
          </p>
        </div>
        <button
          type="button"
          className="icon-button"
          aria-label="Close templates"
          onClick={onClose}
        >
          <Icon name="close" />
        </button>
      </div>
      <label className="template-search">
        <Icon name="search" />
        <input
          autoFocus
          aria-label="Search templates"
          placeholder="Search technologies…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </label>
      <div className="template-filters" aria-label="Template categories">
        {["All", "Frontend", "Backend", "Container", "Custom"].map((c) => (
          <button
            type="button"
            key={c}
            aria-pressed={category === c}
            onClick={() => setCategory(c)}
          >
            {c}
          </button>
        ))}
      </div>
      <div className="template-grid">
        {filtered.map((t) => (
          <button
            type="button"
            className={`template-tile${current === t.id ? " selected" : ""}`}
            key={t.id}
            onClick={() => onSelect(t)}
            aria-label={`Use ${t.name}`}
            aria-pressed={current === t.id}
          >
            <div className="template-tile-top">
              <TemplateIcon slug={t.icon} />
              <span className="template-markers">
                {detected.includes(t.id) && (
                  <span className="template-detected">Detected</span>
                )}
                {current === t.id && <Icon name="check" />}
              </span>
            </div>
            <strong>{t.name}</strong>
            <span className="small muted">{t.description}</span>
          </button>
        ))}
      </div>
      {!filtered.length && (
        <p className="empty small muted">No templates match your search.</p>
      )}
      <div className="pipeline-modal-foot">
        One pipeline. Every container in your project.
      </div>
    </dialog>
  );
}
