"""Real Docker integration. Uses isolated daemon data and cleans up containers."""
import http.cookiejar
import json
import os
import re
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.request
from urllib.parse import urlsplit

root = Path(__file__).resolve().parent.parent
work = Path(tempfile.mkdtemp(prefix="startship-e2e-", dir=Path.home()))
app_ids = []
project_ids = []
service_ids = []
volume_ids = []
build_limits_seen = False
deployments = {}
apps_by_name = {}
deploy_times = {}
test_limits_seen = set()
process = None
log = (work / "daemon.log").open("w")
dashboard_port = os.getenv("STARTSHIP_E2E_DASHBOARD_PORT", "18080")
proxy_port = os.getenv("STARTSHIP_E2E_HTTP_PORT", "18081")
https_port = os.getenv("STARTSHIP_E2E_HTTPS_PORT", "18443")
proxy_url = "http://127.0.0.1:" + proxy_port
api = "http://127.0.0.1:" + dashboard_port + "/api/"
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

def request(path, body=None, method=None):
    req = urllib.request.Request(api + path, data=json.dumps(body).encode() if body is not None else None,
                                 headers={"Content-Type": "application/json"}, method=method)
    with client.open(req, timeout=10) as response:
        return json.load(response)

def inspect_if_present(container_id):
    result = subprocess.run(["docker", "inspect", container_id], text=True, capture_output=True)
    if result.returncode:
        if "no such object" in result.stderr.lower() or "no such container" in result.stderr.lower():
            return None
        raise RuntimeError(result.stderr)
    return json.loads(result.stdout)[0]

def wait_deploy(app, options=None, action="deploy"):
    global build_limits_seen
    started = time.monotonic()
    deployment = request("apps/" + app["id"] + "/" + action, options or {})
    for _ in range(600):
        tests = subprocess.check_output(["docker", "ps", "-q", "--filter", "label=startship.test=" + app["id"]], text=True).split()
        test_container = inspect_if_present(tests[0]) if tests else None
        if test_container:
            config = test_container["HostConfig"]
            assert config["Memory"] == 536870912 and config["NanoCpus"] == 1000000000 and config["PidsLimit"] == 512, config
            assert config["CapDrop"] == ["ALL"] and "no-new-privileges" in config["SecurityOpt"], config
            assert config["NetworkMode"] == "startship-" + app["project_id"], config
            test_limits_seen.add(app["name"])
        builders = subprocess.check_output(["docker", "ps", "-q", "--filter", "name=buildx_buildkit_startship-build-" + app["project_id"]], text=True).split()
        if builders and not build_limits_seen:
            config = json.loads(subprocess.check_output(["docker", "inspect", builders[0]], text=True))[0]["HostConfig"]
            assert config["Memory"] == 1073741824, config
            assert config["CpuPeriod"] == 100000 and config["CpuQuota"] == 100000, config
            print("build worker limits: " + json.dumps({k: config[k] for k in ("Memory", "CpuPeriod", "CpuQuota")}))
            build_limits_seen = True
        result = request("deployments/" + deployment["id"])
        if result["status"] == "running":
            deployments[app["name"]] = result
            deploy_times[result["id"]] = time.monotonic() - started
            print("deployment: running (" + app["name"] + ")")
            return result
        if result["status"] == "failed":
            raise RuntimeError(result.get("log"))
        time.sleep(1)
    raise TimeoutError("deployment did not complete")

try:
    repo = work / "repo"
    repo.mkdir()
    (repo / "app.py").write_text('import os\nfrom http.server import BaseHTTPRequestHandler, HTTPServer\n'
        'class Handler(BaseHTTPRequestHandler):\n'
        ' def do_GET(self):\n'
        '  self.send_response(200); self.end_headers(); self.wfile.write(("hello from Startship python " + self.path + "\\n").encode())\n'
        'HTTPServer(("0.0.0.0", int(os.getenv("PORT", "8080"))), Handler).serve_forever()\n')
    (repo / "requirements.txt").write_text("")
    (repo / "public").mkdir()
    (repo / "public" / "index.html").write_text("hello from Startship static\n")
    (repo / "dockerapp").mkdir()
    (repo / "dockerapp" / "app.py").write_text((repo / "app.py").read_text().replace("Startship python", "Startship Dockerfile"))
    (repo / "dockerapp" / "Dockerfile").write_text('FROM python:3.13-slim\nCOPY app.py /app.py\nRUN python -c "print(123)"\nCMD ["python", "/app.py"]\n')
    (repo / "rustapp" / "src").mkdir(parents=True)
    (repo / "rustapp" / "Cargo.toml").write_text('[package]\nname = "startship-e2e"\nversion = "0.1.0"\nedition = "2021"\n')
    (repo / "rustapp" / "src" / "main.rs").write_text('''use std::{io::{Read, Write}, net::TcpListener};
fn main() {
    let port = std::env::var("PORT").unwrap();
    for stream in TcpListener::bind(format!("0.0.0.0:{port}")).unwrap().incoming() {
        let mut stream = stream.unwrap();
        let mut buffer = [0; 8192];
        let size = stream.read(&mut buffer).unwrap();
        let request = String::from_utf8_lossy(&buffer[..size]);
        let path = request.split_whitespace().nth(1).unwrap_or("/");
        let body = format!("hello from Startship Rust {path}\\n");
        let response = format!("HTTP/1.1 200 OK\\r\\nContent-Length: {}\\r\\nConnection: close\\r\\n\\r\\n{}", body.len(), body);
        let _ = stream.write_all(response.as_bytes());
    }
}
''')
    for args in (["init", "-b", "main"], ["add", "."],
                 ["-c", "user.name=Startship E2E", "-c", "user.email=e2e@localhost", "commit", "-m", "fixture"]):
        subprocess.run(["git", "-C", str(repo)] + args, check=True, stdout=subprocess.DEVNULL)
    environment = dict(os.environ, STARTSHIP_DATA=str(work / "data"), STARTSHIP_DASHBOARD_ADDR="127.0.0.1:" + dashboard_port,
                       STARTSHIP_SOCKET=str(work / "startship.sock"),
                       STARTSHIP_HTTP_ADDR="127.0.0.1:" + proxy_port, STARTSHIP_HTTPS_ADDR="127.0.0.1:" + https_port, STARTSHIP_ADMIN_USER="admin",
                       STARTSHIP_ADMIN_PASSWORD="test-password", STARTSHIP_ACME_EMAIL="", STARTSHIP_DASHBOARD_DOMAIN="")
    process = subprocess.Popen([str(root / "startshipd")], env=environment, stdout=log, stderr=log)
    for _ in range(60):
        try:
            request("setup")
            break
        except Exception:
            if process.poll() is not None:
                raise RuntimeError((work / "daemon.log").read_text())
            time.sleep(0.2)
    request("login", {"username": "admin", "password": "test-password"})
    ws = request("workspaces")[0]
    project = request("projects", {"workspace_id": ws["id"], "name": "e2e", "git_url": repo.as_uri(), "branch": "main"})
    project_ids.append(project["id"])
    detected = request("detect", {"project_id": project["id"]})
    assert {(c["root_dir"], c["build_type"]) for c in detected} == {
        (".", "python"), ("public", "static"), ("dockerapp", "dockerfile"), ("rustapp", "rust")}, detected
    for name, directory, prefix in (("python", ".", "/"), ("static", "public", "/static/"),
                                     ("dockerfile", "dockerapp", "/dockerfile/"), ("rust", "rustapp", "/rust/")):
        app = request("apps", {"project_id": project["id"], "name": name,
                              "root_dir": directory, "build_type": "auto", "port": 8080,
                              "test_command": "sleep 3; python -c 'print(123)'" if name == "python" else "sleep 3; test -f index.html" if name == "static" else ""})
        app_ids.append(app["id"])
        apps_by_name[name] = app
        first_deploy = wait_deploy(app)
        route = request("routes", {"project_id": project["id"], "app_id": app["id"], "host": "test.local",
                                   "path_prefix": prefix, "strip_prefix": name in ("static", "dockerfile")})
        public_url = urlsplit(route["url"])
        assert (public_url.scheme, public_url.hostname, public_url.port, public_url.path) == ("http", "test.local", int(proxy_port), prefix), route
        if name == "python":
            builders_before = subprocess.check_output(["docker", "ps", "-q", "--filter", "name=buildx_buildkit_startship-build-" + project["id"]], text=True).split()
            cached = wait_deploy(app)
            assert "CACHED" in cached["log"], cached["log"]
            builders_after = subprocess.check_output(["docker", "ps", "-q", "--filter", "name=buildx_buildkit_startship-build-" + project["id"]], text=True).split()
            assert builders_before == builders_after and len(builders_after) == 1
            config = json.loads(subprocess.check_output(["docker", "inspect", builders_after[0]], text=True))[0]["HostConfig"]
            assert config["Memory"] == 1073741824 and config["CpuPeriod"] == 100000 and config["CpuQuota"] == 100000, config
            print(f'cache hits and same bounded project builder: OK; first={deploy_times[first_deploy["id"]]:.2f}s second={deploy_times[cached["id"]]:.2f}s')
            first = request("apps/" + app["id"])["active_deployment"]
            patch = urllib.request.Request(api + "apps/" + app["id"],
                    data=json.dumps({"test_command": "exit 1"}).encode(),
                    headers={"Content-Type": "application/json"}, method="PATCH")
            client.open(patch).close()
            failed = request("apps/" + app["id"] + "/deploy", {})
            for _ in range(120):
                result = request("deployments/" + failed["id"])
                if result["status"] == "failed":
                    break
                time.sleep(1)
            assert result["status"] == "failed"
            assert request("apps/" + app["id"])["active_deployment"] == first
            print("failed tests preserve active deployment: OK")
            patch = urllib.request.Request(api + "apps/" + app["id"],
                    data=json.dumps({"test_command": "python -c 'print(123)'"}).encode(),
                    headers={"Content-Type": "application/json"}, method="PATCH")
            client.open(patch).close()
            replacement = wait_deploy(app)
            assert request("deployments/" + first)["status"] == "superseded"
            previous = request("deployments/" + first)["container"]
            for _ in range(50):
                if subprocess.run(["docker", "inspect", previous], stdout=subprocess.DEVNULL,
                                  stderr=subprocess.DEVNULL).returncode != 0:
                    break
                time.sleep(0.2)
            else:
                raise RuntimeError("superseded container was not removed")
            assert inspect_if_present(previous) is None
            print("ephemeral container inspection tolerates removal between ps and inspect: OK")
            print("atomic replacement and old-container removal: OK")
            request("apps/" + app["id"], {"env": {"REDEPLOY_PROBE": "current"}, "port": 8082, "test_command": "exit 1"}, "PATCH")
            restarted = wait_deploy(app, action="redeploy")
            assert restarted["image"] == replacement["image"] and restarted["container"] != replacement["container"]
            assert restarted["redeploy_of"] == replacement["id"] and restarted["commit"] == replacement["commit"]
            assert "Redeploying release " in restarted["log"] and "Cloning into" not in restarted["log"] and "building with" not in restarted["log"]
            inspected = json.loads(subprocess.check_output(["docker", "inspect", restarted["container"]], text=True))[0]
            assert "REDEPLOY_PROBE=current" in inspected["Config"]["Env"] and "PORT=8082" in inspected["Config"]["Env"]
            assert inspected["Config"]["Image"] == replacement["image"]
            rolled_back = wait_deploy(app, {"deployment_id": first_deploy["id"]}, "redeploy")
            assert rolled_back["image"] == first_deploy["image"] and rolled_back["redeploy_of"] == first_deploy["id"]
            assert "building with" not in rolled_back["log"]
            print("redeploy and older-release rollback reuse image, skip clone/build/tests and use current env/port: OK")
        if name == "static":
            restarted = wait_deploy(app, action="redeploy")
            assert restarted["static_dir"] == first_deploy["static_dir"] and Path(restarted["static_dir"]).is_dir()
            assert restarted["redeploy_of"] == first_deploy["id"] and "building with" not in restarted["log"]
            print("static redeploy preserves shared static directory: OK")
        if name == "dockerfile":
            uncached = wait_deploy(app, {"no_cache": True})
            steps = dict(re.findall(r"^(#\d+) \[\d+/\d+\] (.+)$", uncached["log"], re.MULTILINE))
            cached_steps = re.findall(r"^(#\d+) CACHED$", uncached["log"], re.MULTILINE)
            assert uncached["no_cache"] and all(steps.get(step, "").startswith("FROM ") for step in cached_steps), uncached["log"]
            assert "RUN python" in uncached["log"] and " 123\n" in uncached["log"], uncached["log"]
            print("rebuild without cache reruns COPY/RUN with no CACHED executable steps (base image reuse allowed): OK")
    checks = (("/?query=python", "hello from Startship python /?query=python"), ("/static/", "hello from Startship static"),
              ("/static/spa/page", "hello from Startship static"),
              ("/dockerfile/probe?via=router", "hello from Startship Dockerfile /probe?via=router"),
              ("/rust/probe?via=router", "hello from Startship Rust /rust/probe?via=router"))
    for path, expected in checks:
        result = subprocess.check_output(["curl", "-fsS", "-H", "Host: test.local", proxy_url + path], text=True)
        print("curl " + path + ": " + result.strip())
        assert expected in result
    # A Dockerfile build failure must not replace the healthy release either.
    healthy = deployments["dockerfile"]
    dockerfile = repo / "dockerapp" / "Dockerfile"
    original_dockerfile = dockerfile.read_text()
    dockerfile.write_text(original_dockerfile + "RUN exit 42\n")
    for args in (["add", "."], ["-c", "user.name=Startship E2E", "-c", "user.email=e2e@localhost", "commit", "-m", "broken build"]):
        subprocess.run(["git", "-C", str(repo)] + args, check=True, stdout=subprocess.DEVNULL)
    docker_app = next(a for a in request("apps") if a["name"] == "dockerfile")
    failed = request("apps/" + docker_app["id"] + "/deploy", {})
    for _ in range(180):
        result = request("deployments/" + failed["id"])
        if result["status"] == "failed":
            break
        time.sleep(1)
    assert result["status"] == "failed", result
    assert request("apps/" + docker_app["id"])["active_deployment"] == healthy["id"]
    assert "exit code: 42" in result["log"], result["log"]
    print("failed Dockerfile build preserves active deployment: OK")
    dockerfile.write_text(original_dockerfile)
    for args in (["add", "."], ["-c", "user.name=Startship E2E", "-c", "user.email=e2e@localhost", "commit", "-m", "restore build"]):
        subprocess.run(["git", "-C", str(repo)] + args, check=True, stdout=subprocess.DEVNULL)
    # No application ports are published; requests above must pass through Startship.
    for app_id in app_ids:
        ids = subprocess.check_output(["docker", "ps", "-q", "--filter", "label=startship.app=" + app_id], text=True).split()
        if ids:
            inspected = json.loads(subprocess.check_output(["docker", "inspect"] + ids, text=True))
            assert all(not container["HostConfig"].get("PortBindings") for container in inspected)
            for container in inspected:
                config = container["HostConfig"]
                assert config["Memory"] == 536870912 and config["NanoCpus"] == 1000000000 and config["PidsLimit"] == 512, config
                assert config["CapDrop"] == ["ALL"], config
                assert {cap.removeprefix("CAP_") for cap in config["CapAdd"]} == {"CHOWN", "SETUID", "SETGID", "NET_BIND_SERVICE"}, config
                assert "no-new-privileges" in config["SecurityOpt"], config
                assert not config["Privileged"] and not container["Mounts"], container
                assert set(container["NetworkSettings"]["Networks"]) == {"startship-" + project["id"]}, container
                deployment = next(d for d in deployments.values() if d.get("container") == container["Name"].lstrip("/"))
                assert deployment["network"] == "startship-" + project["id"]
                name = next(name for name, d in deployments.items() if d == deployment)
                assert name in container["NetworkSettings"]["Networks"][deployment["network"]]["Aliases"]
    config = json.loads(subprocess.check_output(["docker", "inspect", deployments["python"]["container"]], text=True))[0]["HostConfig"]
    print("app HostConfig: " + json.dumps({k: config[k] for k in ("Memory", "NanoCpus", "PidsLimit", "CapDrop", "CapAdd", "SecurityOpt", "NetworkMode", "Privileged", "Binds")}))
    print("app limits, capabilities, no-new-privileges and project-only networks: OK")
    assert build_limits_seen, "no bounded build worker observed"
    assert test_limits_seen == {"python", "static"}, test_limits_seen
    print("static and app test containers have limits, dropped caps, no-new-privileges and project network: OK")
    other = request("projects", {"workspace_id": ws["id"], "name": "isolated", "git_url": repo.as_uri(), "branch": "main"})
    project_ids.append(other["id"])
    other_app = request("apps", {"project_id": other["id"], "name": "outsider", "root_dir": "dockerapp", "build_type": "dockerfile", "port": 8080})
    app_ids.append(other_app["id"])
    outsider = wait_deploy(other_app)
    assert outsider["network"] == "startship-" + other["id"]
    probe = 'import socket; s=socket.create_connection(("dockerfile",8080),2); s.close(); print("sibling alias connection: OK")'
    print(subprocess.check_output(["docker", "exec", deployments["python"]["container"], "python", "-c", probe], text=True).strip())
    probe = ('import socket\ntry:\n s=socket.create_connection((' + repr(outsider["ip"]) + ',8080),2)\n'
             'except OSError as e:\n print("cross-project connection blocked:", type(e).__name__, str(e))\n'
             'else:\n s.close(); raise SystemExit("UNSAFE: cross-project connection succeeded")\n')
    print(subprocess.check_output(["docker", "exec", deployments["python"]["container"], "python", "-c", probe], text=True).strip())
    request("apps/" + other_app["id"], method="DELETE")
    request("projects/" + other["id"], method="DELETE")
    builders = subprocess.check_output(["docker", "buildx", "ls", "--format", "{{.Name}}"], text=True)
    assert "startship-build-" + other["id"] not in builders
    assert subprocess.run(["docker", "network", "inspect", "startship-" + other["id"]], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode != 0
    print("project delete removes persistent builder and network: OK")
    status = subprocess.check_output(["curl", "-sS", "-o", os.devnull, "-w", "%{http_code}",
                                      proxy_url + "/static/"], text=True)
    assert status == "404", status
    # Verify every route is restored from SQLite after daemon restart.
    process.terminate()
    process.wait(timeout=15)
    process = subprocess.Popen([str(root / "startshipd")], env=environment, stdout=log, stderr=log)
    for _ in range(60):
        try:
            for path, expected in checks:
                result = subprocess.check_output(["curl", "-fsS", "-H", "Host: test.local", proxy_url + path], stderr=subprocess.DEVNULL, text=True)
                assert expected in result, (path, result)
            print("restart routing: OK")
            break
        except Exception:
            time.sleep(0.2)
    else:
        raise RuntimeError("restart routing failed")
    old_builder = subprocess.check_output(["docker", "ps", "-q", "--filter", "name=buildx_buildkit_startship-build-" + project["id"]], text=True).split()
    process.terminate()
    process.wait(timeout=15)
    environment.update(STARTSHIP_BUILD_MEMORY="768m", STARTSHIP_BUILD_CPUS="0.5")
    process = subprocess.Popen([str(root / "startshipd")], env=environment, stdout=log, stderr=log)
    for _ in range(60):
        try:
            request("setup")
            break
        except Exception:
            time.sleep(0.2)
    sibling = request("apps", {"project_id": project["id"], "name": "concurrent", "root_dir": "dockerapp", "build_type": "dockerfile", "port": 8080})
    app_ids.append(sibling["id"])
    queued = [request("apps/" + a["id"] + "/deploy", {}) for a in (sibling, docker_app)]
    for _ in range(180):
        results = [request("deployments/" + d["id"]) for d in queued]
        assert all(d["status"] != "failed" for d in results), results
        if all(d["status"] == "running" for d in results):
            break
        time.sleep(1)
    assert all(d["status"] == "running" for d in results), results
    new_builder = subprocess.check_output(["docker", "ps", "-q", "--filter", "name=buildx_buildkit_startship-build-" + project["id"]], text=True).split()
    assert len(new_builder) == 1 and new_builder != old_builder
    config = json.loads(subprocess.check_output(["docker", "inspect", new_builder[0]], text=True))[0]["HostConfig"]
    assert config["Memory"] == 805306368 and config["CpuQuota"] == 50000 and config["CpuPeriod"] == 100000, config
    print("changed build limits replace builder; concurrent sibling builds safely reuse one bounded builder: OK")
    result = subprocess.check_output(["curl", "-fsS", "-H", "Host: test.local", proxy_url + "/dockerfile/"], text=True)
    assert "hello from Startship Dockerfile" in result
    # The dashboard's project unit discovers multiple HTTP containers without app CRUD.
    grouped_repo = work / "grouped"
    grouped_repo.mkdir()
    (grouped_repo / "package.json").write_text('{"private":true,"workspaces":["services/*"]}')
    for name in ("api", "web"):
        directory = grouped_repo / "services" / name
        directory.mkdir(parents=True)
        (directory / "app.py").write_text((repo / "app.py").read_text().replace("Startship python", "grouped " + name) + '\n')
        (directory / "Dockerfile").write_text('FROM python:3.13-slim\nWORKDIR /app\nCOPY app.py .\nEXPOSE 8080\nCMD ["python", "app.py"]\n')
    def commit_grouped():
        for args in (["add", "."], ["-c", "user.name=Startship E2E", "-c", "user.email=e2e@localhost", "commit", "-m", "grouped fixture"]):
            subprocess.run(["git", "-C", str(grouped_repo)] + args, check=True, stdout=subprocess.DEVNULL)
    subprocess.run(["git", "-C", str(grouped_repo), "init", "-b", "main"], check=True, stdout=subprocess.DEVNULL)
    commit_grouped()
    grouped = request("projects", {"workspace_id": ws["id"], "name": "Grouped", "git_url": grouped_repo.as_uri(), "env": {"SHARED": "project"}})
    project_ids.append(grouped["id"])
    def deploy_project(options=None, action="deploy", expected="running"):
        queued = request("projects/" + grouped["id"] + "/" + action, options or {})
        for _ in range(240):
            release = request("releases/" + queued["id"])
            if release["status"] in ("running", "failed"):
                assert release["status"] == expected, release.get("log")
                return release
            time.sleep(0.5)
        raise TimeoutError("project release did not complete")
    with socket.socket() as port_probe:
        port_probe.bind(("127.0.0.1", 0))
        service_port = port_probe.getsockname()[1]
    service = request("workspaces/" + ws["id"] + "/services",
                      {"name": "e2e-cache", "engine": "redis", "external_port": service_port})
    service_ids.append(service["id"])
    volume_ids.append(service["volume_id"])
    for _ in range(180):
        service = request("services/" + service["id"])
        if service["status"] == "running":
            break
        assert service["status"] != "failed", service.get("last_error")
        time.sleep(1)
    else:
        raise TimeoutError("Redis service did not start: " + service.get("last_error", ""))
    with socket.create_connection(("127.0.0.1", service_port), timeout=5) as redis:
        stream = redis.makefile("rb")
        redis.sendall(("AUTH " + service["config"]["password"] + "\r\n").encode())
        assert stream.readline() == b"+OK\r\n"
        redis.sendall(b"*1\r\n$4\r\nPING\r\n")
        assert stream.readline() == b"+PONG\r\n"
    print("Redis service running via API and external host-port PING: +PONG")
    request("services/" + service["id"] + "/attachments", {"project_id": grouped["id"], "alias": "shared-cache"})
    project_volume = request("workspaces/" + ws["id"] + "/volumes",
                             {"name": "e2e-data", "project_id": grouped["id"], "mount_path": "/e2e-data"})
    volume_ids.append(project_volume["id"])
    first_group = deploy_project()
    containers = request("containers?project_id=" + grouped["id"])
    app_ids.extend(c["id"] for c in containers)
    assert len(containers) == 2 and len(first_group["container_releases"]) == 2
    assert first_group["log"].count("Cloning into") == 1
    assert request("projects/" + grouped["id"])["active_deployment"] == first_group["id"]
    for container in containers:
        revision = request("deployments/" + container["active_deployment"])
        assert revision["release_id"] == first_group["id"] and revision["commit"] == first_group["commit"]
        info = inspect_if_present(revision["container"])
        assert "SHARED=project" in info["Config"]["Env"]
        assert "SHARED_CACHE_HOST=shared-cache.local" in info["Config"]["Env"]
        probe = ('import os,socket; host=os.environ["SHARED_CACHE_HOST"]; '
                 'assert host=="shared-cache.local"; '
                 'assert socket.gethostbyname(host)==socket.gethostbyname("e2e-cache.local"); '
                 's=socket.create_connection((host,int(os.environ["SHARED_CACHE_PORT"])),5); '
                 's.sendall(("AUTH "+os.environ["SHARED_CACHE_PASSWORD"]+"\\r\\n").encode()); assert s.recv(1024)==b"+OK\\r\\n"; '
                 's.sendall(b"PING\\r\\n"); assert s.recv(1024)==b"+PONG\\r\\n"; s.close()')
        subprocess.run(["docker", "exec", revision["container"], "python", "-c", probe], check=True)
        assert any(mount["Name"] == "startship-vol-" + project_volume["id"] and mount["Destination"] == "/e2e-data"
                   for mount in info["Mounts"]), info["Mounts"]
        assert set(info["NetworkSettings"]["Networks"]) == {"startship-" + grouped["id"]}
        route = request("routes", {"project_id": grouped["id"], "container_id": container["id"], "host": "grouped.local",
                                  "path_prefix": "/api" if container["root_dir"].endswith("api") else "/", "strip_prefix": container["root_dir"].endswith("api")})
        assert route["container_id"] == container["id"]
        assert "log" in request("containers/" + container["id"] + "/logs")
    def grouped_response(path):
        return subprocess.check_output(["curl", "-fsS", "-H", "Host: grouped.local", proxy_url + path], text=True)
    assert "grouped web /" in grouped_response("/")
    assert "grouped api /probe" in grouped_response("/api/probe")
    print("project deployment: one checkout, two detected HTTP containers, one shared release/env, selectable domain targets and runtime logs: OK")
    first_revision = request("deployments/" + first_group["container_releases"][0])
    subprocess.run(["docker", "exec", first_revision["container"], "python", "-c",
                    'from pathlib import Path; Path("/e2e-data/probe").write_text("persistent-e2e-data")'], check=True)
    print("project deployment injects service env, resolves alias/service DNS and reaches Redis: OK")
    failing_file = grouped_repo / "services" / "web" / "Dockerfile"
    healthy_file = failing_file.read_text()
    failing_file.write_text(healthy_file.replace('CMD ', 'RUN false\nCMD '))
    commit_grouped()
    failed_group = deploy_project(expected="failed")
    assert request("projects/" + grouped["id"])["active_deployment"] == first_group["id"]
    assert "grouped api /probe" in grouped_response("/api/probe") and "grouped web /" in grouped_response("/")
    for revision_id in failed_group["container_releases"]:
        revision = request("deployments/" + revision_id)
        if revision.get("container"):
            assert inspect_if_present(revision["container"]) is None
    print("failed grouped deployment preserves every previous container and removes staged runtimes: OK")
    request("projects/" + grouped["id"], {"env": {"SHARED": "current"}, "test_command": "exit 1"}, "PATCH")
    restarted_group = deploy_project({"deployment_id": first_group["id"]}, "redeploy")
    assert restarted_group["redeploy_of"] == first_group["id"] and "Cloning into" not in restarted_group["log"]
    for revision_id in restarted_group["container_releases"]:
        revision = request("deployments/" + revision_id)
        assert revision["container"] != first_revision["container"]
        subprocess.run(["docker", "exec", revision["container"], "python", "-c",
                        'from pathlib import Path; assert Path("/e2e-data/probe").read_text()=="persistent-e2e-data"'], check=True)
        assert "SHARED=current" in inspect_if_present(revision["container"])["Config"]["Env"]
    print("project volume file survives redeploy into replacement app containers: OK")
    request("services/" + service["id"], method="DELETE")
    assert inspect_if_present("startship-svc-" + service["id"]) is None
    retained = next(v for v in request("workspaces/" + ws["id"] + "/volumes")["volumes"] if v["id"] == service["volume_id"])
    assert not retained.get("service_id")
    subprocess.run(["docker", "volume", "inspect", "startship-vol-" + service["volume_id"]], check=True, stdout=subprocess.DEVNULL)
    print("deleting service removes container and retains its persistent Docker/API volume: OK")
    request("volumes/" + service["volume_id"], method="DELETE")
    request("volumes/" + project_volume["id"], {"project_id": "", "mount_path": ""}, "PATCH")
    process.terminate()
    process.wait(timeout=15)
    process = subprocess.Popen([str(root / "startshipd")], env=environment, stdout=log, stderr=log)
    for _ in range(60):
        try:
            assert "grouped api /probe" in grouped_response("/api/probe")
            assert request("releases/" + restarted_group["id"])["status"] == "running"
            break
        except Exception:
            time.sleep(0.2)
    else:
        raise RuntimeError("grouped release did not survive restart")
    print("grouped artifact redeploy skips clone/build/tests, uses shared current env and survives daemon restart: OK")
    # Custom pipeline test expectations and HTTP health checking use project releases.
    custom_pipeline = {
        "build": {"image": "python:3.13-slim", "commands": []},
        "test": {"commands": ["echo pipeline-output"], "expect_exit_code": 0, "expect_output": "missing-output"},
        "deploy": {"command": "python app.py", "port": 8080, "health_path": "/health", "expect_status": 200, "health_timeout": 15},
    }
    custom = request("projects", {"workspace_id": ws["id"], "name": "Custom pipeline", "git_url": repo.as_uri(),
                                  "build_type": "custom", "pipeline": custom_pipeline})
    project_ids.append(custom["id"])
    def pipeline_release(expected):
        queued = request("projects/" + custom["id"] + "/deploy", {})
        for _ in range(240):
            release = request("releases/" + queued["id"])
            if release["status"] in ("running", "failed"):
                assert release["status"] == expected, release.get("log")
                return release
            time.sleep(0.5)
        raise TimeoutError("custom pipeline release did not complete")
    failed_pipeline = pipeline_release("failed")
    assert 'test expected output containing "missing-output"' in failed_pipeline["log"], failed_pipeline["log"]
    custom_pipeline["test"]["expect_output"] = "pipeline-output"
    request("projects/" + custom["id"], {"pipeline": custom_pipeline}, "PATCH")
    successful_pipeline = pipeline_release("running")
    custom_containers = request("containers?project_id=" + custom["id"])
    app_ids.extend(c["id"] for c in custom_containers)
    revision = request("deployments/" + successful_pipeline["container_releases"][0])
    assert revision["pipeline"]["deploy"]["health_path"] == "/health"
    assert revision["pipeline"]["deploy"]["health_timeout"] == 15
    custom_pipeline["deploy"]["expect_status"] = 201
    custom_pipeline["deploy"]["health_timeout"] = 1
    request("projects/" + custom["id"], {"pipeline": custom_pipeline}, "PATCH")
    timed_out = pipeline_release("failed")
    assert "health check timed out after 1s" in timed_out["log"], timed_out["log"]
    runtime_logs = subprocess.check_output(["docker", "logs", revision["container"]], text=True, stderr=subprocess.STDOUT)
    assert 'GET /health HTTP/1.1' in runtime_logs, runtime_logs
    ip = revision["ip"]
    assert subprocess.check_output(["curl", "-fsS", "http://" + ip + ":8080/health"], text=True)
    print("custom pipeline expected output fails then passes, HTTP health path succeeds: OK")
    engine_selection = os.getenv("STARTSHIP_E2E_ENGINES")
    engines = ("nixpacks", "buildpacks") if engine_selection == "1" else (engine_selection,) if engine_selection in ("nixpacks", "buildpacks") else ()
    if engines:
        engine_repo = work / "engine-source"
        engine_repo.mkdir()
        (engine_repo / "package.json").write_text(json.dumps({"name": "engine-fixture", "version": "1.0.0", "private": True,
            "engines": {"node": "22.x"}, "scripts": {"start": "node server.js"}}))
        (engine_repo / "server.js").write_text("require('http').createServer((req,res)=>res.end(process.argv.includes('--override')?'engine override':'engine default')).listen(process.env.PORT || 3000, '0.0.0.0');\n")
        (engine_repo / "package-lock.json").write_text(json.dumps({"name": "engine-fixture", "version": "1.0.0", "lockfileVersion": 3,
            "requires": True, "packages": {"": json.loads((engine_repo / "package.json").read_text())}}))
        for args in (["init", "-b", "main"], ["add", "."], ["-c", "user.name=Startship E2E", "-c", "user.email=e2e@localhost", "commit", "-m", "engine fixture"]):
            subprocess.run(["git", "-C", str(engine_repo)] + args, check=True, stdout=subprocess.DEVNULL)
        def wait_engine_release(queued, status):
            for _ in range(3600):
                release = request("releases/" + queued["id"])
                if release["status"] in ("running", "failed"):
                    assert release["status"] == status, release.get("log")
                    return release
                time.sleep(0.5)
            raise TimeoutError(engine + " did not complete:\n" + release.get("log", "")[-5000:])

        for engine in engines:
            engine_pipeline = {"build": {"engine": engine, "image": "ignored image", "commands": ["false"], "dockerfile": "../ignored"},
                "test": {"commands": ["node -e \"console.log('engine-test')\""], "expect_exit_code": 0, "expect_output": "engine-test"},
                "deploy": {"port": 3000, "health_path": "/health", "expect_status": 200}}
            if engine == "nixpacks":
                engine_pipeline["test"]["image"] = "mirror.gcr.io/library/node:22-alpine"
            else:
                engine_pipeline["test"]["commands"] = ["node -e \"console.log('engine-test ' + process.version); console.log(JSON.stringify(process.argv.slice(1)))\" 'a b' '' '$HOME' '$(exit 99)'"]
                engine_pipeline["test"]["expect_output"] = '["a b","","$HOME","$(exit 99)"]'
                engine_pipeline["deploy"]["command"] = "node -e \"require('./server.js')\" -- --override"
            engine_project = request("projects", {"workspace_id": ws["id"], "name": "Engine " + engine,
                "git_url": engine_repo.as_uri(), "branch": "main", "build_type": "custom", "pipeline": engine_pipeline})
            project_ids.append(engine_project["id"])
            queued = request("projects/" + engine_project["id"] + "/deploy", {})
            engine_release = wait_engine_release(queued, "running")
            engine_containers = request("containers?project_id=" + engine_project["id"])
            app_ids.extend(c["id"] for c in engine_containers)
            revision = request("deployments/" + engine_release["container_releases"][0])
            assert revision["pipeline"]["build"]["engine"] == engine
            assert all(header in revision["log"] for header in ("==> 1/3 Build (" + engine + ")", "==> 2/3 Test", "engine-test", "==> 3/3 Deploy")), revision["log"]
            request("routes", {"project_id": engine_project["id"], "container_id": engine_containers[0]["id"], "host": engine + ".local"})
            result = subprocess.check_output(["curl", "-fsS", "-H", "Host: " + engine + ".local", proxy_url + "/"], text=True)
            assert result == ("engine default" if engine == "nixpacks" else "engine override"), result
            workers = subprocess.check_output(["docker", "ps", "-q", "--filter", "name=buildx_buildkit_startship-build-" + engine_project["id"]], text=True).split()
            assert len(workers) == 1, workers
            config = inspect_if_present(workers[0])["HostConfig"]
            assert (config["Memory"], config["CpuPeriod"], config["CpuQuota"]) == (805306368, 100000, 50000), config
            print(engine + " deployment: Running; 3 stages, tests, snapshot and bounded builder OK; proxy curl: " + result)
            if engine == "buildpacks":
                assert "engine-test v" in revision["log"] and "syntax error" not in revision["log"], revision["log"]
                print("buildpacks quoting-heavy Test and Deploy.Command executed verbatim: OK")
                engine_pipeline["test"]["commands"].append("exit 3")
                engine_pipeline["test"]["expect_output"] = "engine-test"
                request("projects/" + engine_project["id"], {"pipeline": engine_pipeline}, "PATCH")
                failed = wait_engine_release(request("projects/" + engine_project["id"] + "/deploy", {}), "failed")
                failed_revision = request("deployments/" + failed["container_releases"][0])
                assert "test expected exit code 0, got 3" in failed_revision["log"], failed_revision["log"]
                assert "engine-test v" in failed_revision["log"] and "==> 3/3 Deploy" not in failed_revision["log"], failed_revision["log"]
                assert request("projects/" + engine_project["id"])["active_deployment"] == engine_release["id"]
                assert subprocess.check_output(["curl", "-fsS", "-H", "Host: buildpacks.local", proxy_url + "/"], text=True) == result
                print("buildpacks exit 3: stage failed (test expected exit code 0, got 3), matching output did not mask failure; Deploy skipped and healthy release preserved: OK")
    else:
        print("engine deployments: SKIP (set STARTSHIP_E2E_ENGINES=1; pinned Nixpacks and large CNB downloads required)")
    if os.getenv("STARTSHIP_E2E_SERVICES") == "1":
        from services_e2e import run_services
        run_services(request)
    print("E2E PASS")
finally:
    if process and process.poll() is None:
        process.terminate()
        process.wait(timeout=15)
    for service_id in service_ids:
        subprocess.run(["docker", "rm", "-f", "startship-svc-" + service_id], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for app_id in app_ids:
        ids = subprocess.check_output(["docker", "ps", "-aq", "--filter", "label=startship.app=" + app_id], text=True).split()
        if ids:
            subprocess.run(["docker", "rm", "-f"] + ids, check=True, stdout=subprocess.DEVNULL)
        images = subprocess.check_output(["docker", "images", "-q", "startship/" + app_id], text=True).split()
        if images:
            subprocess.run(["docker", "rmi", "-f"] + images, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for volume_id in volume_ids:
        subprocess.run(["docker", "volume", "rm", "startship-vol-" + volume_id], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for project_id in project_ids:
        builders = [json.loads(line)["Name"] for line in subprocess.check_output(["docker", "buildx", "ls", "--format", "json"], text=True).splitlines() if line.strip()]
        for builder in builders:
            if builder.startswith("startship-build-" + project_id + "-"):
                subprocess.run(["docker", "buildx", "rm", "-f", builder], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run(["docker", "network", "rm", "startship-" + project_id], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    log.close()
    shutil.rmtree(work)
