"""Opt-in real clients for every catalog engine; never logs credentials."""
import base64
import http.cookiejar
import json
import os
import socket
import ssl
import subprocess
import time
import urllib.request
from urllib.parse import urlencode


def docker(args, data=None, environment=None, timeout=120):
    result = subprocess.run(["docker"] + args, input=data, text=True, capture_output=True,
                            env=dict(os.environ, **(environment or {})), timeout=timeout)
    if result.returncode:
        raise RuntimeError(result.stdout + result.stderr)
    return result.stdout.strip()


def run_services(request):
    client_image = "startship-catalog-clients:smoke"
    docker(["build", "-t", client_image, "-"],
           'FROM python:3.13-slim\nRUN pip install --no-cache-dir "psycopg[binary]==3.2.9" "PyMySQL==1.1.1" "pymongo==4.13.2" "redis==6.2.0" "pika==1.3.2"\n', timeout=600)
    workspace = request("workspaces", {"name": "Catalog smoke " + str(int(time.time()))})
    project = request("projects", {"workspace_id": workspace["id"], "name": "catalog-clients"})
    label = "startship.catalog-smoke=" + workspace["id"]
    results = []

    def url_client(env, script):
        return docker(["run", "--rm", "--label", label, "--network", "startship-" + project["id"], "-e", "CHECK_INPUT",
                       client_image, "python", "-c", "import json,os; c=json.loads(os.environ['CHECK_INPUT']); " + script],
                      environment={"CHECK_INPUT": json.dumps(env)})

    try:
        engines = request("catalog")
        selected = os.getenv("STARTSHIP_SERVICES_ENGINES", "").split(",")
        for engine in engines:
            if selected != [""] and engine["id"] not in selected:
                continue
            service = None
            item = {"engine": engine["id"], "version": engine["versions"][0], "started": False}
            try:
                with socket.socket() as probe:
                    probe.bind(("127.0.0.1", 0))
                    port = probe.getsockname()[1]
                service = request("workspaces/" + workspace["id"] + "/services",
                                  {"name": "smoke-" + engine["id"], "engine": engine["id"], "external_port": port})
                deadline = time.monotonic() + 600
                while time.monotonic() < deadline:
                    service = request("services/" + service["id"])
                    if service["status"] == "running":
                        item["started"] = True
                        break
                    if service["status"] == "failed":
                        raise RuntimeError(service["last_error"])
                    time.sleep(1)
                else:
                    raise TimeoutError("service did not reach running")
                request("services/" + service["id"] + "/attachments", {"project_id": project["id"], "alias": "probe"})
                attached = request("projects/" + project["id"] + "/services")["attached"]
                env = next(a["env"] for a in attached if a["service"]["id"] == service["id"])
                config, image = service["config"], engine["image"] + ":" + service["version"]

                def client(command, data=None, environment=None, network="host", client_image=image):
                    args = ["run", "--rm", "--label", label, "-i", "--network", network]
                    for key in environment or {}:
                        args += ["-e", key]
                    return docker(args + ["--entrypoint", command[0], client_image] + command[1:], data, environment)

                def check():
                    kind = engine["id"]
                    if kind == "postgresql":
                        out = client(["psql", "-h", "127.0.0.1", "-p", str(port), "-U", config["user"], "-d", config["database"], "-Atc", "SELECT 1"], environment={"PGPASSWORD": config["password"]})
                        assert out == "1", out
                        url_client(env, 'import psycopg; db=psycopg.connect(c["PROBE_URL"]); assert db.execute("SELECT 1").fetchone()==(1,); print("postgres URL SELECT 1: 1")')
                        return "psql SELECT 1: 1; generated URL SELECT 1: 1"
                    if kind in ("mysql", "mariadb"):
                        out = client(["mysql" if kind == "mysql" else "mariadb", "-h", "127.0.0.1", "-P", str(port), "-u", config["user"], "-D", config["database"], "-Nse", "SELECT 1"], environment={"MYSQL_PWD": config["password"]})
                        assert out == "1", out
                        url_client(env, 'from urllib.parse import urlsplit,unquote; import pymysql; u=urlsplit(c["PROBE_URL"]); db=pymysql.connect(host=u.hostname,port=u.port,user=unquote(u.username),password=unquote(u.password),database=unquote(u.path.lstrip("/"))); cursor=db.cursor(); cursor.execute("SELECT 1"); assert cursor.fetchone()==(1,); print("mysql URL SELECT 1: 1")')
                        return kind + " configured user/database SELECT 1: 1; generated URL SELECT 1: 1"
                    if kind == "mongodb":
                        out = client(["mongosh", "--quiet", "--host", "127.0.0.1", "--port", str(port), "-u", config["user"], "-p", config["password"], "--authenticationDatabase", "admin", config["database"], "--eval", 'if(db.runCommand({ping:1}).ok!==1)throw Error("ping");db.smoke.insertOne({ok:1});print("mongosh authenticated ping + insert: OK")'])
                        assert "mongosh authenticated ping + insert: OK" in out, out
                        url_client(env, 'import pymongo; db=pymongo.MongoClient(c["PROBE_URL"],serverSelectionTimeoutMS=5000).get_default_database(); assert db.command("ping")["ok"]==1; db.url_probe.insert_one({"ok":1}); print("mongo URL ping + insert: OK")')
                        return "mongosh authenticated ping + insert: OK; generated URL ping + insert: OK"
                    if kind in ("redis", "valkey"):
                        assert config.get("password"), "catalog does not generate a password; AUTH cannot be verified"
                        out = client(["redis-cli" if kind == "redis" else "valkey-cli", "--no-auth-warning", "-h", "127.0.0.1", "-p", str(port), "-a", config["password"], "PING"])
                        assert out == "PONG", out
                        rejected = client(["redis-cli" if kind == "redis" else "valkey-cli", "-h", "127.0.0.1", "-p", str(port), "PING"])
                        assert "NOAUTH" in rejected, "unauthenticated cache request accepted"
                        url_client(env, 'import redis; assert redis.Redis.from_url(c["PROBE_URL"]).ping(); print("redis URL authenticated PING: PONG")')
                        return "AUTH + PING: PONG; generated URL PING: PONG"
                    if kind == "memcached":
                        out = client(["nc", "-w", "3", "127.0.0.1", str(port)], "set probe 0 60 5\r\nvalue\r\nget probe\r\nquit\r\n", client_image="mirror.gcr.io/library/busybox:1.37")
                        assert "STORED" in out and "VALUE probe 0 5\nvalue\nEND" in out, out
                        return "nc set/get: STORED; VALUE probe 0 5; value; END"
                    if kind == "rabbitmq":
                        url_client(env, 'import pika; connection=pika.BlockingConnection(pika.URLParameters(c["PROBE_URL"])); channel=connection.channel(); channel.queue_declare(queue="probe"); channel.basic_publish(exchange="",routing_key="probe",body=b"authenticated"); method,properties,body=channel.basic_get("probe",auto_ack=True); assert body==b"authenticated"; connection.close(); print("AMQP URL authenticated publish/consume: OK")')
                        return "AMQP generated URL authenticated publish/consume: OK"
                    if kind == "kafka":
                        for network, host, label in [("host", "127.0.0.1:" + str(port), "host"), ("startship-" + project["id"], "probe.local:9092", "alias")]:
                            topic = "smoke-" + label
                            client(["/opt/kafka/bin/kafka-topics.sh", "--bootstrap-server", host, "--create", "--if-not-exists", "--topic", topic, "--partitions", "1", "--replication-factor", "1"], network=network)
                            client(["/opt/kafka/bin/kafka-console-producer.sh", "--bootstrap-server", host, "--topic", topic], "message-" + label + "\n", network=network)
                            out = client(["/opt/kafka/bin/kafka-console-consumer.sh", "--bootstrap-server", host, "--topic", topic, "--from-beginning", "--max-messages", "1", "--timeout-ms", "15000"], network=network)
                            assert "message-" + label in out, out
                        return "host AND project-alias topic create + produce/consume: OK"
                    if kind == "nats":
                        with socket.create_connection(("127.0.0.1", port), timeout=5) as connection:
                            assert connection.recv(4096).startswith(b"INFO ")
                            connection.sendall(b'CONNECT {"verbose":false}\r\nPING\r\n')
                            assert b"PONG" in connection.recv(4096)
                        return "NATS CONNECT + PING: PONG (no authentication configured)"
                    if kind == "clickhouse":
                        url = "http://127.0.0.1:" + str(port) + "/?" + urlencode({"database": config["database"], "query": "SELECT 1"})
                        req = urllib.request.Request(url, headers={"Authorization": "Basic " + base64.b64encode((config["user"] + ":" + config["password"]).encode()).decode()})
                        with urllib.request.urlopen(req, timeout=5) as response:
                            assert response.read().strip() == b"1"
                        return "authenticated HTTP SELECT 1: 1"
                    if kind == "seaweedfs":
                        credentials = {"AWS_ACCESS_KEY_ID": config["access_key"], "AWS_SECRET_ACCESS_KEY": config["secret_key"], "AWS_DEFAULT_REGION": "us-east-1", "AWS_EC2_METADATA_DISABLED": "true"}
                        endpoint="http://127.0.0.1:"+str(port)
                        def aws(args,data=None,internal=False):
                            return client(["aws","--endpoint-url",env["PROBE_ENDPOINT"] if internal else endpoint]+args,data,credentials,
                                          network="startship-"+project["id"] if internal else "host",client_image="amazon/aws-cli:2.27.49")
                        aws(["s3api","list-buckets"])
                        aws(["s3","mb","s3://catalog-probe"])
                        aws(["s3","cp","-","s3://catalog-probe/probe.txt"],"persistent-s3-data\n")
                        out=aws(["s3","ls","s3://catalog-probe"])
                        assert "probe.txt" in out,out
                        try:
                            urllib.request.urlopen(endpoint+"/catalog-probe/probe.txt",timeout=5)
                        except urllib.error.HTTPError as error:
                            assert error.code==403,error
                        else:
                            raise AssertionError("anonymous S3 object read accepted")
                        request("services/"+service["id"]+"/restart",{})
                        for _ in range(30):
                            try:
                                assert aws(["s3","cp","s3://catalog-probe/probe.txt","-"],internal=True)=="persistent-s3-data"
                                break
                            except RuntimeError:
                                time.sleep(2)
                        else:
                            raise AssertionError("S3 object did not survive service restart")
                        return "aws-cli mb + put + ls: probe.txt; anonymous: 403; restart/project-endpoint get: persistent-s3-data"
                    if kind == "opensearch":
                        assert config.get("password"), "catalog disables security and generates no credentials"
                        auth = base64.b64encode((config["user"] + ":" + config["password"]).encode()).decode()
                        url = "https://127.0.0.1:" + str(port) + "/"
                        with urllib.request.urlopen(urllib.request.Request(url, headers={"Authorization": "Basic " + auth}), context=ssl._create_unverified_context(), timeout=5) as response:
                            body = json.load(response)
                            assert body["version"]["number"] == service["version"], body
                        try:
                            urllib.request.urlopen(url, context=ssl._create_unverified_context(), timeout=5)
                        except urllib.error.HTTPError as error:
                            assert error.code == 401, error
                        else:
                            raise AssertionError("unauthenticated OpenSearch request accepted")
                        return "authenticated HTTPS GET /: " + body["version"]["number"] + "; unauthenticated: 401"
                    raise AssertionError("no client check for " + kind)

                last = None
                ready_deadline = time.monotonic() + 180
                while time.monotonic() < ready_deadline:
                    try:
                        item["result"] = check()
                        break
                    except Exception as error:
                        last = error
                        if "catalog " in str(error):
                            raise
                        time.sleep(2)
                else:
                    raise RuntimeError(str(last))
            except Exception as error:
                message = str(error)
                for value in (service or {}).get("config", {}).values():
                    if len(value) > 12:
                        message = message.replace(value, "[REDACTED]")
                item["error"] = message[-3000:]
            finally:
                if service:
                    request("services/" + service["id"] + "?delete_volume=true", method="DELETE")
            results.append(item)
            print(json.dumps(item), flush=True)
    finally:
        clients = docker(["ps", "-aq", "--filter", "label=" + label]).split()
        if clients:
            docker(["rm", "-f"] + clients)
        for remaining in request("workspaces/" + workspace["id"] + "/services")["services"]:
            request("services/" + remaining["id"] + "?delete_volume=true", method="DELETE")
        for volume in request("workspaces/" + workspace["id"] + "/volumes")["volumes"]:
            request("volumes/" + volume["id"], method="DELETE")
        request("projects/" + project["id"], method="DELETE")
        request("workspaces/" + workspace["id"], method="DELETE")
        docker(["image", "rm", client_image])
        print("catalog smoke workspace/services/volumes/project/client image cleaned up", flush=True)
    assert results and all("result" in item for item in results), "catalog clients failed"
    print("CATALOG CLIENTS PASS: " + str(len(results)) + "/" + str(len(engines)), flush=True)


if __name__ == "__main__":
    base = os.getenv("STARTSHIP_SERVICES_API", "http://127.0.0.1:18080/api/")
    client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def request(path, body=None, method=None):
        req = urllib.request.Request(base + path, data=json.dumps(body).encode() if body is not None else None,
                                     headers={"Content-Type": "application/json"}, method=method)
        with client.open(req, timeout=180) as response:
            return json.load(response)

    request("login", {"username": os.environ["STARTSHIP_SERVICES_USER"], "password": os.environ["STARTSHIP_SERVICES_PASSWORD"]})
    run_services(request)
