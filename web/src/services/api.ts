export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
let sessionRevision = 0;
let rotatingCredentials = false;
let renewal: {
  controller: AbortController;
  promise: Promise<boolean>;
} | null = null;
export function beginSessionChange() {
  sessionRevision++;
  renewal?.controller.abort();
  renewal = null;
}
export function setCredentialRotation(value: boolean) {
  rotatingCredentials = value;
  beginSessionChange();
  window.dispatchEvent(
    new CustomEvent("credential-rotation", { detail: value }),
  );
}

export function refreshAccess(): Promise<boolean> {
  if (renewal) return renewal.promise;
  const controller = new AbortController();
  const promise = fetch("/api/refresh", {
    method: "POST",
    credentials: "same-origin",
    signal: controller.signal,
  })
    .then((response) => {
      if (response.status === 401) return false;
      if (!response.ok)
        throw new ApiError("Could not renew access", response.status);
      return true;
    })
    .finally(() => {
      if (renewal?.controller === controller) renewal = null;
    });
  renewal = { controller, promise };
  return promise;
}

export async function request<T>(
  path: string,
  body?: unknown,
  method = body === undefined ? "GET" : "POST",
  signal?: AbortSignal,
): Promise<T> {
  const revision = sessionRevision;
  const send = () =>
    fetch(`/api/${path}`, {
      method,
      credentials: "same-origin",
      signal,
      headers:
        body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  let response = await send();
  const protectedRequest = !["login", "setup", "logout", "refresh"].includes(
    path,
  );
  const mayRenew =
    protectedRequest &&
    revision === sessionRevision &&
    (!rotatingCredentials || (path === "me" && method === "PATCH"));
  if (response.status === 401 && mayRenew && !signal?.aborted) {
    if (await refreshAccess()) {
      if (revision !== sessionRevision || signal?.aborted)
        throw new DOMException("Request cancelled", "AbortError");
      response = await send();
    }
  }
  const data = response.status === 204 ? undefined : await response.json();
  if (!response.ok) {
    if (
      response.status === 401 &&
      revision === sessionRevision &&
      protectedRequest &&
      !rotatingCredentials &&
      !signal?.aborted
    )
      window.dispatchEvent(new Event("session-expired"));
    throw new ApiError(data?.error || "Request failed", response.status);
  }
  return data as T;
}
export const remove = (path: string) =>
  request<void>(path, undefined, "DELETE");
