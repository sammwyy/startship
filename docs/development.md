# Build and test Startship

Install the [source build prerequisites](../INSTALL.md#build-from-source). A clean checkout must build the dashboard before running Go tooling that imports it, because the daemon embeds the generated Vite assets.

```sh
make build
npm run format:check --prefix web
npm test --prefix web
go vet ./...
go test -race ./...
```

Run `./scripts/e2e.sh` for Docker-backed deployment tests. They need Python 3, curl, Git, and a running Docker Engine and buildx. Use a development host: the tests start real containers and builders. Set `STARTSHIP_E2E_SERVICES=1` to also exercise the service catalog; this pulls additional images.

For dashboard development, run the daemon with its dashboard bound to port 8080 and start `npm run dev --prefix web`. Vite forwards HTTP and WebSocket API requests to the daemon.

| Path | Contents |
| --- | --- |
| [`cmd/startshipd`](../cmd/startshipd) and [`cmd/startship`](../cmd/startship) | Daemon and CLI entry points |
| [`internal/server`](../internal/server) | HTTP API, access checks, deployment orchestration, and Docker integration |
| [`internal/cli`](../internal/cli) | CLI commands and transport |
| [`internal/pipeline`](../internal/pipeline) and [`internal/detect`](../internal/detect) | Templates and build-root detection |
| [`web/src`](../web/src) | React dashboard |
| [`scripts`](../scripts) | Docker-backed integration tests |
