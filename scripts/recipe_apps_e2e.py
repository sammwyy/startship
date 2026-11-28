import http.cookiejar
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


IMAGE = "mirror.gcr.io/library/busybox:1.37"
SCHEMA = {"type": "object", "properties": {"message": {"type": "string", "default": "recipe app working"}}}
RECIPE = {
    "id": "e2e-web", "name": "E2E Web", "version": "1", "description": "Two-container test recipe.",
    "containers": [
        {
            "id": "web", "image": IMAGE, "mounts": {"data": "/www"},
            "command": ["httpd", "-f", "-p", "8080", "-h", "/www"],
            "healthcheck": "wget -q -O - http://127.0.0.1:8080/settings.json",
        },
        {"id": "worker", "image": IMAGE, "command": ["sleep", "3600"]},
    ],
    "volumes": ["data"],
    "config_schema": {"type": "object", "properties": {}},
    "files": [{"id": "settings", "volume": "data", "path": "settings.json", "format": "json", "schema": SCHEMA}],
    "http": [{"id": "http", "container": "web", "port": 8080}],
    "ports": [{"id": "public", "container": "web", "port": 8080, "protocol": "tcp", "host_port": 0}],
}


class RegistryHandler(BaseHTTPRequestHandler):
    requests = []
    unavailable = False

    def do_GET(self):
        self.requests.append(self.path)
        if self.unavailable:
            self.send_error(503)
            return
        self.send_response(200)
        self.end_headers()
        index = {"recipes": [RECIPE]} if self.path == "/recipes" else {
            "engines": [{"id": "redis", "name": "Remote Redis", "category": "cache",
                         "image": "redis", "versions": ["7.4"], "port": 6379}]
        }
        self.wfile.write(json.dumps(index).encode())

    def log_message(self, *_args):
        pass


def port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def docker(*args, check=True):
    return subprocess.run(["docker", *args], check=check, text=True, capture_output=True, timeout=90)


def wait(predicate, timeout=80):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            result = predicate()
            if result:
                return result
        except urllib.error.URLError:
            pass
        time.sleep(0.25)
    raise AssertionError("timed out waiting for " + str(predicate))


def delete_app(request, path):
    deadline = time.monotonic() + 15
    while True:
        try:
            return request(path, method="DELETE")
        except urllib.error.HTTPError as error:
            with error:
                if error.code != 409 or json.load(error).get("error") != "app is busy" or time.monotonic() >= deadline:
                    raise
            time.sleep(0.25)


def run():
    docker("pull", IMAGE)
    if docker("image", "inspect", "busybox:1.37", check=False).returncode:
        docker("tag", IMAGE, "busybox:1.37")
    with tempfile.TemporaryDirectory(prefix="startship-recipes-e2e-") as data, ThreadingHTTPServer(("127.0.0.1", 0), RegistryHandler) as registry:
        threading.Thread(target=registry.serve_forever, daemon=True).start()
        dashboard, proxy, https, published = port(), port(), port(), port()
        env = dict(os.environ, STARTSHIP_DATA=data, STARTSHIP_SOCKET=data + "/startship.sock",
                   STARTSHIP_DASHBOARD_ADDR=f"127.0.0.1:{dashboard}", STARTSHIP_HTTP_ADDR=f"127.0.0.1:{proxy}",
                   STARTSHIP_HTTPS_ADDR=f"127.0.0.1:{https}",
                   STARTSHIP_RECIPES_URL=f"http://127.0.0.1:{registry.server_port}/recipes",
                   STARTSHIP_SERVICES_URL=f"http://127.0.0.1:{registry.server_port}/services",
                   STARTSHIP_ADMIN_USER="admin", STARTSHIP_ADMIN_PASSWORD="recipe-test-password")
        client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        apps = []
        proc = None
        log = Path(data, "daemon.log")

        def request(path, body=None, method=None):
            req = urllib.request.Request(f"http://127.0.0.1:{dashboard}/api/{path}", method=method,
                                         data=None if body is None else json.dumps(body).encode(),
                                         headers={"Content-Type": "application/json"})
            with client.open(req, timeout=20) as response:
                return json.load(response)

        def start():
            with log.open("ab") as out:
                process = subprocess.Popen([str(Path(__file__).resolve().parents[1] / "startshipd")],
                                           env=env, stdout=out, stderr=out)
            try:
                wait(lambda: not request("setup")["required"], timeout=12)
                request("login", {"username": "admin", "password": "recipe-test-password"})
            except Exception:
                process.terminate()
                process.wait(timeout=10)
                raise
            return process

        def proxy_get(host):
            req = urllib.request.Request(f"http://127.0.0.1:{proxy}/settings.json", headers={"Host": host})
            with urllib.request.urlopen(req, timeout=3) as response:
                return json.load(response)

        try:
            proc = start()
            assert RegistryHandler.requests == []
            wait(lambda: [r["id"] for r in request("recipes")] == ["e2e-web"])
            assert request("recipes/status")["source"] == "remote"
            assert RegistryHandler.requests == ["/recipes"]
            ws = request("workspaces")[0]["id"]
            wait(lambda: [e["name"] for e in request("catalog")] == ["Remote Redis"])
            assert request(f"catalog/refresh?workspace={ws}", method="POST")["metadata"]["source"] == "remote"
            assert RegistryHandler.requests == ["/recipes", "/services", "/services"]
            RegistryHandler.unavailable = True
            try:
                request(f"recipes/refresh?workspace={ws}", method="POST")
                raise AssertionError("failed refresh accepted")
            except urllib.error.HTTPError as error:
                assert error.code == 502
            assert [r["id"] for r in request("recipes")] == ["e2e-web"]
            base = f"workspaces/{ws}/apps"
            for name, configured_port in [("one", published), ("two", 0)]:
                app = request(base, {"name": name, "recipe": "e2e-web", "ports": {"public": configured_port}})
                apps.append(app["id"])
            one, two = apps

            def reaches(id, desired):
                result = request(f"{base}/{id}")
                if result["status"] == "failed":
                    raise AssertionError(result.get("error"))
                return result["status"] == desired and not result.get("action")

            wait(lambda: reaches(one, "running"))
            wait(lambda: reaches(two, "running"))
            one_network, two_network = ("startship-app-" + id for id in apps)
            worker, web_two = one_network + "-worker", two_network + "-web"
            peer = docker("exec", worker, "wget", "-qO-", "http://web:8080/settings.json")
            assert json.loads(peer.stdout)["message"] == "recipe app working"
            for network, id in [(one_network, one), (two_network, two)]:
                for container in ("web", "worker"):
                    inspect = json.loads(docker("inspect", network + "-" + container).stdout)[0]
                    assert set(inspect["NetworkSettings"]["Networks"]) == {network}, inspect
                    assert inspect["Config"]["Labels"]["startship.recipe-app"] == id
            other_ip = json.loads(docker("inspect", web_two).stdout)[0]["NetworkSettings"]["Networks"][two_network]["IPAddress"]
            isolated = docker("exec", worker, "wget", "-T", "2", "-qO-", f"http://{other_ip}:8080/settings.json", check=False)
            assert isolated.returncode != 0, "another app's private IP is reachable"
            with urllib.request.urlopen(f"http://127.0.0.1:{published}/settings.json", timeout=3) as web:
                assert json.load(web)["message"] == "recipe app working"
            try:
                request(base, {"name": "collision", "recipe": "e2e-web", "ports": {"public": published}})
                raise AssertionError("duplicate port accepted")
            except urllib.error.HTTPError as e:
                assert e.code == 409, e

            path = f"{base}/{one}"
            host = "recipes.localtest.me"
            request(path + "/domains", {"endpoint": "http", "host": host})
            assert wait(lambda: proxy_get(host))["message"] == "recipe app working"
            file_path = path + "/files/settings"
            assert request(file_path)["message"] == "recipe app working"
            request(file_path, {"values": {"message": "persisted"}, "restart": True}, "PUT")
            wait(lambda: reaches(one, "running"))
            assert proxy_get(host)["message"] == "persisted"
            request(path + "/stop", {}, "POST")
            wait(lambda: reaches(one, "stopped"))
            worker_files = path + "/containers/worker/files"
            caps = request(worker_files + "/list")["capabilities"]
            assert caps == {"write": True, "move": False, "delete": False, "reason": "The container is stopped."}, caps
            request(worker_files + "/create?path=%2Fexplorer.txt", method="POST")
            assert request(worker_files + "/text?path=%2Fexplorer.txt")["text"] == ""
            try:
                request(worker_files + "/delete?path=%2Fexplorer.txt", method="DELETE")
                raise AssertionError("delete unexpectedly accepted on a stopped container")
            except urllib.error.HTTPError as error:
                assert error.code == 409, error
            app_volume = request(path + "/volumes/data/files/list")
            assert app_volume["capabilities"]["delete"] and any(e["name"] == "settings.json" for e in app_volume["entries"]), app_volume
            print("file explorer stopped App container and App volume capabilities: OK")
            request(path + "/start", {}, "POST")
            wait(lambda: reaches(one, "running"))
            assert request(file_path)["message"] == "persisted"
            proc.terminate()
            proc.wait(timeout=15)
            proc = start()
            assert [r["id"] for r in request("recipes")] == ["e2e-web"]
            assert [e["name"] for e in request("catalog")] == ["Remote Redis"]
            assert RegistryHandler.requests == ["/recipes", "/services", "/services", "/recipes"]
            assert wait(lambda: proxy_get(host))["message"] == "persisted"
            delete_app(request, path)
            docker("volume", "inspect", one_network + "-vol-data")
            delete_app(request, f"{base}/{two}?purge=true")
            assert docker("volume", "inspect", two_network + "-vol-data", check=False).returncode
            print("Recipe apps E2E PASS: remote registry, private networks, public port, domain, config, lifecycle and volume retention")
        except Exception:
            print(log.read_text()[-5000:])
            raise
        finally:
            if proc and proc.poll() is None:
                proc.terminate()
                proc.wait(timeout=15)
            for app_id in apps:
                n = "startship-app-" + app_id
                docker("rm", "-fv", n + "-web", n + "-worker", check=False)
                docker("network", "rm", n, check=False)
                docker("volume", "rm", n + "-vol-data", check=False)
            registry.shutdown()


if __name__ == "__main__":
    run()
