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

root = Path(__file__).resolve().parent.parent
work = Path(tempfile.mkdtemp(prefix="harbor-e2e-", dir=Path.home()))
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
    for _ in range(240):
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
        '  self.send_response(200); self.end_headers(); self.wfile.write(b"hello from Harbor python\\n")\n'
        'HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()\n')
    (repo / "requirements.txt").write_text("")
    (repo / "public").mkdir()
    (repo / "public" / "index.html").write_text("hello from Harbor static\n")
    for args in (["init", "-b", "main"], ["add", "app.py", "requirements.txt", "public/index.html"],
                 ["-c", "user.name=Harbor E2E", "-c", "user.email=e2e@localhost", "commit", "-m", "fixture"]):
        subprocess.run(["git", "-C", str(repo)] + args, check=True, stdout=subprocess.DEVNULL)
    environment = dict(os.environ, HARBOR_DATA=str(work / "data"), HARBOR_DASHBOARD_ADDR="127.0.0.1:18080",
                       HARBOR_HTTP_ADDR="127.0.0.1:18081", HARBOR_ADMIN_USER="admin",
                       HARBOR_ADMIN_PASSWORD="test-password", HARBOR_ACME_EMAIL="", HARBOR_DASHBOARD_DOMAIN="")
    process = subprocess.Popen([str(root / "harbord")], env=environment, stdout=log, stderr=log)
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
    project = request("projects", {"workspace_id": ws["id"], "name": "e2e"})
    detected = request("detect", {"git_url": repo.as_uri()})
    assert {c["build_type"] for c in detected} == {"python", "static"}, detected
    for name, directory, kind, prefix in (("python", ".", "auto", "/"), ("static", "public", "static", "/static/")):
        app = request("apps", {"project_id": project["id"], "name": name, "git_url": repo.as_uri(),
                              "root_dir": directory, "build_type": kind, "port": 8080,
                              "test_command": "python -c 'print(123)'" if name == "python" else "test -f index.html"})
        app_ids.append(app["id"])
        wait_deploy(app)
        request("routes", {"project_id": project["id"], "app_id": app["id"], "host": "test.local",
                           "path_prefix": prefix, "strip_prefix": name == "static"})
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
            assert subprocess.run(["docker", "inspect", previous], stdout=subprocess.DEVNULL,
                                  stderr=subprocess.DEVNULL).returncode != 0
            print("atomic replacement and old-container removal: OK")
    for path, expected in (("/", "hello from Harbor python"), ("/static/", "hello from Harbor static"),
                           ("/static/spa/page", "hello from Harbor static")):
        result = subprocess.check_output(["curl", "-fsS", "-H", "Host: test.local", "http://127.0.0.1:18081" + path], text=True)
        print("curl " + path + ": " + result.strip())
        assert expected in result
    # Verify routing is restored from SQLite after daemon restart.
    process.terminate()
    process.wait(timeout=15)
    process = subprocess.Popen([str(root / "harbord")], env=environment, stdout=log, stderr=log)
    for _ in range(60):
        try:
            result = subprocess.check_output(["curl", "-fsS", "-H", "Host: test.local", "http://127.0.0.1:18081/"], stderr=subprocess.DEVNULL, text=True)
            assert "hello from Harbor python" in result
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
        ids = subprocess.check_output(["docker", "ps", "-aq", "--filter", "label=harbor.app=" + app_id], text=True).split()
        if ids:
            subprocess.run(["docker", "rm", "-f"] + ids, check=True, stdout=subprocess.DEVNULL)
        images = subprocess.check_output(["docker", "images", "-q", "harbor/" + app_id], text=True).split()
        if images:
            subprocess.run(["docker", "rmi", "-f"] + images, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    log.close()
    shutil.rmtree(work)
