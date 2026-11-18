import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useQueryClient } from "@tanstack/react-query";
import { reloadDashboard } from "../lib/dashboardReload";
import {
  LiveConnection,
  applyLiveMessage,
  type LiveStatus,
  type LiveSubscription,
} from "../services/live";
import { queries } from "../services/queries";
import type { SystemVersion } from "../services/types";

const Context = createContext<{
  connection: LiveConnection | null;
  status: LiveStatus;
}>({ connection: null, status: "disconnected" });
export function LiveProvider({ children }: { children: ReactNode }) {
  const client = useQueryClient();
  const [status, setStatus] = useState<LiveStatus>("connecting");
  const daemonVersion = useRef<SystemVersion>(undefined);
  const connection = useMemo(
    () =>
      new LiveConnection(
        (spec, message) => {
          applyLiveMessage(client, spec, message);
          if (
            message.type === "error" &&
            (message.error === "forbidden" || message.error === "not found")
          )
            void client.resetQueries({
              predicate: (query) =>
                !["me", "setup"].includes(String(query.queryKey[0])),
            });
        },
        setStatus,
        (version) => {
          const previous = daemonVersion.current;
          if (
            previous &&
            (version.version !== previous.version ||
              version.commit !== previous.commit)
          ) {
            reloadDashboard();
            return;
          }
          daemonVersion.current = version;
          void client.cancelQueries({
            queryKey: queries.version().queryKey,
            exact: true,
          });
          client.setQueryData(queries.version().queryKey, version);
        },
      ),
    [client],
  );
  useEffect(() => {
    connection.connect();
    const rotate = (event: Event) => {
      if ((event as CustomEvent<boolean>).detail) connection.disconnect();
      else connection.connect();
    };
    window.addEventListener("credential-rotation", rotate);
    return () => {
      window.removeEventListener("credential-rotation", rotate);
      connection.disconnect();
    };
  }, [connection]);
  return (
    <Context.Provider value={{ connection, status }}>
      {children}
    </Context.Provider>
  );
}
export const useLiveStatus = () => useContext(Context).status;
export function useLiveFeed<T>(spec: LiveSubscription | null) {
  const { connection, status } = useContext(Context);
  const key = JSON.stringify(spec);
  const [state, setState] = useState<{
    key: string;
    data?: T;
    error?: Error;
  }>();
  useEffect(() => {
    if (!connection || key === "null") return;
    return connection.subscribe(JSON.parse(key), (event) => {
      if (event.type === "snapshot") setState({ key, data: event.data as T });
      if (event.type === "error")
        setState({
          key,
          error: new Error(event.error || "Live data unavailable"),
        });
    });
  }, [connection, key]);
  return { ...(state?.key === key ? state : undefined), status };
}
export function useLiveSubscription(spec: LiveSubscription | null) {
  const { connection } = useContext(Context);
  const [error, setError] = useState<Error | null>(null);
  const key = JSON.stringify(spec);
  useEffect(() => {
    setError(null);
    if (!connection || key === "null") return;
    return connection.subscribe(JSON.parse(key), (event) => {
      if (event.type === "error")
        setError(new Error(event.error || "Live updates unavailable"));
      else setError(null);
    });
  }, [connection, key]);
  return error;
}
export function useLiveProjects(ids: string[]) {
  const { connection } = useContext(Context);
  const key = JSON.stringify(ids);
  useEffect(() => {
    if (!connection) return;
    const unsubscribes = (JSON.parse(key) as string[]).map((project_id) =>
      connection.subscribe({ topic: "project", project_id }),
    );
    return () => unsubscribes.forEach((unsubscribe) => unsubscribe());
  }, [connection, key]);
}
