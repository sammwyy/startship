"""Real Docker integration. Uses isolated daemon data and cleans up containers."""
import http.cookiejar
import json
import os
import re
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import urllib.request
from urllib.parse import urlsplit

root = Path(__file__).resolve().parent.parent
work = Path(tempfile.mkdtemp(prefix="startship-e2e-", dir=Path.home()))
app_ids = []
project_ids = []
build_limits_seen = False
deployments = {}
apps_by_name = {}
deploy_times = {}
test_limits_seen = set()
process = None
log = (work / "daemon.log").open("w")
api = "http://127.0.0.1:18080/api/"
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

def request(path, body=None, method=None):
    req = urllib.request.Request(api + path, data=json.dumps(body).encode() if body is not None else None,
                                 headers={"Content-Type": "application/json"}, method=method)
    with client.open(req, timeout=10) as response:
        return json.load(response)

def wait_deploy(app, options=None, action="deploy"):
    global build_limits_seen
    started = time.monotonic()
    deployment = request("apps/" + app["id"] + "/" + action, options or {})
    for _ in range(600):
        tests = subprocess.check_output(["docker", "ps", "-q", "--filter", "label=startship.test=" + app["id"]], text=True).split()
        if tests:
            config = json.loads(subprocess.check_output(["docker", "inspect", tests[0]], text=True))[0]["HostConfig"]
            assert config["Memory"] == 536870912 and config["NanoCpus"] == 1000000000 and config["PidsLimit"] == 512, config
            assert config["CapDrop"] == ["ALL"] and "no-new-privileges" in config["SecurityOpt"], config
            assert config["NetworkMode"] == "startship-" + app["project_id"], config
            test_limits_seen.add(app["name"])
        builders = subprocess.check_output(["docker", "ps", "-q", "--filter", "name=buildx_buildkit_startship-build-"], text=True).split()
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
    environment = dict(os.environ, STARTSHIP_DATA=str(work / "data"), STARTSHIP_DASHBOARD_ADDR="127.0.0.1:18080",
                       STARTSHIP_HTTP_ADDR="127.0.0.1:18081", STARTSHIP_ADMIN_USER="admin",
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
        assert (public_url.scheme, public_url.hostname, public_url.port, public_url.path) == ("http", "test.local", 18081, prefix), route
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
        result = subprocess.check_output(["curl", "-fsS", "-H", "Host: test.local", "http://127.0.0.1:18081" + path], text=True)
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
                                      "http://127.0.0.1:18081/static/"], text=True)
    assert status == "404", status
    # Verify every route is restored from SQLite after daemon restart.
    process.terminate()
    process.wait(timeout=15)
    process = subprocess.Popen([str(root / "startshipd")], env=environment, stdout=log, stderr=log)
    for _ in range(60):
        try:
            for path, expected in checks:
                result = subprocess.check_output(["curl", "-fsS", "-H", "Host: test.local", "http://127.0.0.1:18081" + path], stderr=subprocess.DEVNULL, text=True)
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
    result = subprocess.check_output(["curl", "-fsS", "-H", "Host: test.local", "http://127.0.0.1:18081/dockerfile/"], text=True)
    assert "hello from Startship Dockerfile" in result
    print("E2E PASS")
finally:
    if process and process.poll() is None:
        process.terminate()
        process.wait(timeout=15)
    for app_id in app_ids:
        ids = subprocess.check_output(["docker", "ps", "-aq", "--filter", "label=startship.app=" + app_id], text=True).split()
        if ids:
            subprocess.run(["docker", "rm", "-f"] + ids, check=True, stdout=subprocess.DEVNULL)
        images = subprocess.check_output(["docker", "images", "-q", "startship/" + app_id], text=True).split()
        if images:
            subprocess.run(["docker", "rmi", "-f"] + images, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for project_id in project_ids:
        builders = [json.loads(line)["Name"] for line in subprocess.check_output(["docker", "buildx", "ls", "--format", "json"], text=True).splitlines() if line.strip()]
        for builder in builders:
            if builder.startswith("startship-build-" + project_id + "-"):
                subprocess.run(["docker", "buildx", "rm", "-f", builder], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run(["docker", "network", "rm", "startship-" + project_id], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    log.close()
    shutil.rmtree(work)
