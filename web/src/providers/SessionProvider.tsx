import { createContext, useContext, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  beginSessionChange,
  request,
  setCredentialRotation,
} from "../services/api";
import type { Scoped, User } from "../services/types";

interface Session {
  user: User | null;
  loading: boolean;
  setupRequired: boolean;
  error: Error | null;
  temporaryPassword: string;
  login: (username: string, password: string, setup?: boolean) => Promise<void>;
  logout: () => Promise<void>;
  updateAccount: (changes: {
    current_password: string;
    username?: string;
    new_password?: string;
  }) => Promise<void>;
  can: (permission: string, scope?: Scoped) => boolean;
}
const Context = createContext<Session | null>(null);
export const useSession = () => useContext(Context)!;

export function SessionProvider({ children }: { children: ReactNode }) {
  const client = useQueryClient();
  const [temporaryPassword, setTemporaryPassword] = useState("");
  const setup = useQuery({
    queryKey: ["setup"],
    queryFn: () => request<{ required: boolean }>("setup"),
    staleTime: Infinity,
  });
  const me = useQuery({
    queryKey: ["me"],
    queryFn: () => request<User>("me"),
    enabled: !!setup.data && !setup.data.required,
    retry: false,
    staleTime: Infinity,
  });
  useEffect(() => {
    const expire = () => {
      beginSessionChange();
      client.clear();
      client.setQueryData(["setup"], { required: false });
      client.setQueryData(["me"], null);
      setTemporaryPassword("");
    };
    window.addEventListener("session-expired", expire);
    return () => window.removeEventListener("session-expired", expire);
  }, [client]);
  const user = me.data ?? null;
  async function login(username: string, password: string, firstRun = false) {
    if (firstRun) await request("setup", { username, password });
    await request("login", { username, password });
    beginSessionChange();
    client.clear();
    const account = await request<User>("me");
    setTemporaryPassword(account.must_change_password ? password : "");
    client.setQueryData(["setup"], { required: false });
    client.setQueryData(["me"], account);
  }
  async function logout() {
    await request("logout", {});
    beginSessionChange();
    client.clear();
    client.setQueryData(["setup"], { required: false });
    client.setQueryData(["me"], null);
    setTemporaryPassword("");
  }
  async function updateAccount(changes: {
    current_password: string;
    username?: string;
    new_password?: string;
  }) {
    setCredentialRotation(true);
    try {
      await client.cancelQueries();
      const account = await request<Pick<User, "id" | "username">>(
        "me",
        changes,
        "PATCH",
      );
      client.setQueryData<User | null>(["me"], (current) =>
        current
          ? {
              ...current,
              ...account,
              must_change_password: changes.new_password
                ? false
                : current.must_change_password,
            }
          : current,
      );
      setTemporaryPassword("");
      await client.invalidateQueries({ queryKey: ["me"], exact: true });
    } finally {
      setCredentialRotation(false);
    }
  }
  return (
    <Context.Provider
      value={{
        user,
        temporaryPassword,
        login,
        logout,
        updateAccount,
        loading:
          setup.isPending ||
          (!!setup.data && !setup.data.required && me.isPending),
        setupRequired: setup.data?.required ?? false,
        error: setup.error,
        can: (permission, scope) =>
          !!(scope ? scope.permissions : user?.permissions)?.includes(
            permission,
          ),
      }}
    >
      {children}
    </Context.Provider>
  );
}
