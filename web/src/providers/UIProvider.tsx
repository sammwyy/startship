import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { AsyncForm } from "../components/ui";
import { Icon } from "../components/Icon";

interface DialogOptions {
  title: string;
  description?: string;
  fields?: ReactNode;
  submit?: string;
  onSubmit: (form: FormData) => Promise<unknown>;
}
interface UI {
  notify: (text: string) => void;
  dialog: (options: DialogOptions) => void;
  run: (action: () => Promise<unknown>) => void;
  openNavigation: () => void;
  setNavigation: (open: boolean) => void;
  navigationOpen: boolean;
}
const Context = createContext<UI | null>(null);
export const useUI = () => useContext(Context)!;
export function UIProvider({ children }: { children: ReactNode }) {
  const [message, setMessage] = useState("");
  const [options, setOptions] = useState<DialogOptions>();
  const [navigationOpen, setNavigation] = useState(false);
  const modal = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    if (!message) return;
    const timer = window.setTimeout(() => setMessage(""), 3500);
    return () => clearTimeout(timer);
  }, [message]);
  useEffect(() => {
    if (options) modal.current?.showModal();
  }, [options]);
  const close = () => {
    modal.current?.close();
    setOptions(undefined);
  };
  return (
    <Context.Provider
      value={{
        notify: setMessage,
        dialog: setOptions,
        run: (action) => {
          void action().catch((error) =>
            setMessage(error instanceof Error ? error.message : String(error)),
          );
        },
        navigationOpen,
        setNavigation,
        openNavigation: () => setNavigation(true),
      }}
    >
      {children}
      <div
        id="toast"
        role="status"
        style={{ display: message ? "block" : "none" }}
      >
        {message}
      </div>
      <dialog
        ref={modal}
        id="modal"
        aria-labelledby="modal-title"
        onClose={() => setOptions(undefined)}
      >
        {options && (
          <>
            <div className="row">
              <h2 id="modal-title">{options.title}</h2>
              <button
                type="button"
                className="icon-button"
                aria-label="Close"
                onClick={close}
              >
                <Icon name="close" />
              </button>
            </div>
            <p className="small">{options.description}</p>
            <AsyncForm
              key={options.title}
              onSubmit={async (form) => {
                await options.onSubmit(form);
                close();
              }}
            >
              <div className="modal-body">{options.fields}</div>
              <div className="form-actions">
                <button type="button" onClick={close}>
                  Cancel
                </button>
                <button className="primary" type="submit">
                  {options.submit || "Save changes"}
                </button>
              </div>
            </AsyncForm>
          </>
        )}
      </dialog>
    </Context.Provider>
  );
}
