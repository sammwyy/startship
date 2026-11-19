# Deploy your first project

This guide starts with a running daemon and a completed administrator sign-in. If you haven't set those up, start with [Installation](../INSTALL.md).

## 1. Connect a repository

Open your workspace, go to **Projects**, and select **New project**. Give the project a name and enter its Git URL and branch. Leave the root at the repository root unless the app lives in a subdirectory. To connect a private repository with a Git token, use an HTTPS Git URL.

Startship uses one source and one release history per project. With automatic detection, a repository can deploy one runnable app or several detected servers together. In the project, review **Source**, **Pipeline**, and **Environment** before deploying. Start with automatic detection or choose an explicit template or build engine where needed; see [Project pipelines](pipelines.md).

## 2. Deploy and check the result

Open **Deployments** and select **Deploy**. Open the release to see its build output. For a containerized app, listen on `0.0.0.0` at the configured internal port so Startship can reach it. If the build or health check fails, use the release output to diagnose it; a failed release does not replace the preceding healthy release.

Use **Build without cache** when you need a fresh build. **Redeploy selected** starts a previously successful release from its retained artifacts, using the current project environment without cloning or rebuilding.

## 3. Route a domain

1. For a public app, point DNS for a hostname you control at the Startship host. If the workspace doesn't already allow the hostname, have a workspace administrator add it under workspace **Domains**.
2. In the project, open **Domains** and select **Connect domain**. Enter the hostname and a path prefix such as `/`. If your release has more than one container, select the destination. Use the resulting route URL, not the dashboard port, to open your app.
3. To use HTTPS, request or upload a matching certificate under workspace **Certificates**. See [Daemon configuration](configuration.md#https-and-certificates) for ACME prerequisites.

For a local source build with the [installation example](../INSTALL.md#build-from-source), use `localhost` as the hostname and `/` as the path, then open `http://localhost:8081`. A Platform Admin can add the route without a workspace domain claim.

## 4. Add data and automate future releases

- Create a database or cache under workspace **Services**. In the project, use **Connections** to attach it, then redeploy so the running container receives the connection settings. Keep services private unless you need external access.
- Use workspace **Volumes** for persistent storage, and **Backups** to schedule copies for a service backed by a volume.
- To deploy matching GitHub pushes automatically, enable push deployment in the project overview and use the signed webhook URL and secret shown under **Source**. The webhook endpoint must be reachable by GitHub; do not expose raw dashboard port 8080 just for this.

## Sleep when idle

In the project overview, enable **Sleep when idle** and choose idle minutes (1–10080).
This setting applies immediately. Startship stops all runtime containers in the active
release without deleting them; static-only projects cannot enable it. In-flight
requests, including long-lived WebSocket and SSE connections, keep the project awake.
The idle timer starts again when the last request finishes.

The next allowed request wakes the entire project and waits for its health checks
before being proxied. There is no loading page or redirect, so API clients work
normally, but that first request takes longer. A failed wake returns HTTP 503;
the next request retries. The active release displays **Sleeping** while stopped.

From the CLI, use `startship projects update ID '{"sleep_after":15}'` to configure
idle minutes (`0` disables), `startship projects sleep ID` to sleep immediately,
and `startship projects wake ID` to wake. Manual sleep is explicit and can interrupt
in-flight connections. Sleep/wake requires deploy permission; configuring the timer
requires project settings permission.
