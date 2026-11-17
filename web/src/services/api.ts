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
export function beginSessionChange() {
  sessionRevision++;
}
export function setCredentialRotation(value: boolean) {
  rotatingCredentials = value;
  beginSessionChange();
}

export async function request<T>(
  path: string,
  body?: unknown,
  method = body === undefined ? "GET" : "POST",
  signal?: AbortSignal,
): Promise<T> {
  const revision = sessionRevision;
  const response = await fetch(`/api/${path}`, {
    method,
    credentials: "same-origin",
    signal,
    headers:
      body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = response.status === 204 ? undefined : await response.json();
  if (!response.ok) {
    if (
      response.status === 401 &&
      revision === sessionRevision &&
      !rotatingCredentials
    )
      window.dispatchEvent(new Event("session-expired"));
    throw new ApiError(data?.error || "Request failed", response.status);
  }
  return data as T;
}
export const remove = (path: string) =>
  request<void>(path, undefined, "DELETE");
