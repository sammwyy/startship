"""Real Docker integration. Uses isolated daemon data and cleans up containers."""
import http.cookiejar
import json
import os
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
process = None
log = (work / "daemon.log").open("w")
api = "http://127.0.0.1:18080/api/"
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

def request(path, body=None):
    req = urllib.request.Request(api + path, data=json.dumps(body).encode() if body is not None else None,
                                 headers={"Content-Type": "application/json"})
    with client.open(req, timeout=10) as response:
        return json.load(response)

def wait_deploy(app):
    deployment = request("apps/" + app["id"] + "/deploy", {})
    for _ in range(600):
        result = request("deployments/" + deployment["id"])
        if result["status"] == "running":
            print("deployment: running (" + app["name"] + ")")
            return result
        if result["status"] == "failed":
            raise RuntimeError(result.get("log"))
        time.sleep(1)
    raise TimeoutError("deployment did not complete")

try:
    repo = work / "repo"
    repo.mkdir()
    (repo / "app.py").write_text('from http.server import BaseHTTPRequestHandler, HTTPServer\n'
        'class Handler(BaseHTTPRequestHandler):\n'
        ' def do_GET(self):\n'
        '  self.send_response(200); self.end_headers(); self.wfile.write(("hello from Startship python " + self.path + "\\n").encode())\n'
        'HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()\n')
    (repo / "requirements.txt").write_text("")
    (repo / "public").mkdir()
    (repo / "public" / "index.html").write_text("hello from Startship static\n")
    (repo / "dockerapp").mkdir()
    (repo / "dockerapp" / "app.py").write_text((repo / "app.py").read_text().replace("Startship python", "Startship Dockerfile"))
    (repo / "dockerapp" / "Dockerfile").write_text('FROM python:3.13-slim\nWORKDIR /app\nCOPY app.py .\nCMD ["python", "app.py"]\n')
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
    detected = request("detect", {"project_id": project["id"]})
    assert {(c["root_dir"], c["build_type"]) for c in detected} == {
        (".", "python"), ("public", "static"), ("dockerapp", "dockerfile"), ("rustapp", "rust")}, detected
    for name, directory, prefix in (("python", ".", "/"), ("static", "public", "/static/"),
                                     ("dockerfile", "dockerapp", "/dockerfile/"), ("rust", "rustapp", "/rust/")):
        app = request("apps", {"project_id": project["id"], "name": name,
                              "root_dir": directory, "build_type": "auto", "port": 8080,
                              "test_command": "python -c 'print(123)'" if name == "python" else "test -f index.html" if name == "static" else ""})
        app_ids.append(app["id"])
        wait_deploy(app)
        route = request("routes", {"project_id": project["id"], "app_id": app["id"], "host": "test.local",
                                   "path_prefix": prefix, "strip_prefix": name in ("static", "dockerfile")})
        public_url = urlsplit(route["url"])
        assert (public_url.scheme, public_url.hostname, public_url.port, public_url.path) == ("http", "test.local", 18081, prefix), route
        if name == "python":
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
    checks = (("/?query=python", "hello from Startship python /?query=python"), ("/static/", "hello from Startship static"),
              ("/static/spa/page", "hello from Startship static"),
              ("/dockerfile/probe?via=router", "hello from Startship Dockerfile /probe?via=router"),
              ("/rust/probe?via=router", "hello from Startship Rust /rust/probe?via=router"))
    for path, expected in checks:
        result = subprocess.check_output(["curl", "-fsS", "-H", "Host: test.local", "http://127.0.0.1:18081" + path], text=True)
        print("curl " + path + ": " + result.strip())
        assert expected in result
    # No application ports are published; requests above must pass through Startship.
    for app_id in app_ids:
        ids = subprocess.check_output(["docker", "ps", "-q", "--filter", "label=startship.app=" + app_id], text=True).split()
        if ids:
            inspected = json.loads(subprocess.check_output(["docker", "inspect"] + ids, text=True))
            assert all(not container["HostConfig"].get("PortBindings") for container in inspected)
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
    log.close()
    shutil.rmtree(work)
