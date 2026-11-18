#!/usr/bin/env python3
"""Captures the golden responses of the Python Lookup Service.

Runs the scenario README.md describes against two named local clusters: the
lookup cluster, which runs the Lookup Service and plays cluster-a, and the
remote cluster, which plays cluster-b. It applies the resources under
scenario/, sends every request of the scenario to the Lookup Service through
its ingress, and writes one normalized fixture per request under fixtures/,
a snapshot of the training resources the service watches under states/ for
each step, and the capture's provenance to capture.json.

It needs only the Python standard library, kubectl and the educates CLI of
the build under test, and never prompts:

    python3 -I capture.py --lookup-cluster lsg-golden-a --remote-cluster lsg-golden-b

The clusters must be fresh: the scenario starts from portals with no
sessions but the reserved one, and changes the lookup configuration as it
goes. Run it against clusters made for it, and delete them afterwards.
"""

import argparse
import base64
import datetime
import http.client
import json
import os
import re
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import time

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.abspath(os.path.join(SCRIPT_DIR, "..", "..", ".."))

CONFIG_NAMESPACE = "educates-config"
SERVICE_NAMESPACE = "educates"

INDEX_URL = "https://sites.example.test/index"

ADMIN = ("admin-client", "golden-admin-password")
TENANT = ("tenant-client", "golden-tenant-password")
BOTH = ("both-client", "golden-both-password")
USER = ("user-client", "golden-user-password")
REVOKE = ("revoke-client", "golden-revoke-password")
ROTATE = ("rotate-client", "golden-rotate-password")
ROTATED_PASSWORD = "golden-rotate-password-2"
DOOMED = ("doomed-client", "golden-doomed-password")

JWT_PATTERN = re.compile(r"^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$")
ACTIVATION_TOKEN_PATTERN = re.compile(r"token=[A-Za-z0-9]+")
UUID_PATTERN = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")

TOKEN_LIFETIME = 72 * 3600


def log(message):
    print(f"==> {message}", flush=True)


def die(message):
    print(f"capture.py: {message}", file=sys.stderr, flush=True)
    sys.exit(1)


def run(args, input_text=None, check=True):
    result = subprocess.run(
        args, input=input_text, capture_output=True, text=True, check=False
    )
    if check and result.returncode != 0:
        die(f"{' '.join(args)} failed: {result.stderr.strip()}")
    return result


class Cluster:
    """A named local cluster, reached through its own kube context and on its
    listen address."""

    def __init__(self, cli, local_name, role):
        self.cli = cli
        self.local_name = local_name
        self.role = role
        self.context = f"kind-educates-{local_name}"
        rows = run([cli, "local", "cluster", "list"]).stdout.splitlines()[1:]
        for row in rows:
            fields = row.split()
            if len(fields) >= 5 and fields[-1] == self.context:
                self.listen_address, self.domain = fields[-3], fields[-2]
                break
        else:
            die(f"no running local cluster {local_name}")
        self.cafile = None

    def kubectl(self, *args, input_text=None, check=True):
        return run(
            ["kubectl", "--context", self.context, *args],
            input_text=input_text,
            check=check,
        )

    def kubectl_json(self, *args):
        return json.loads(self.kubectl(*args, "-o", "json").stdout)

    def write_ca(self, directory):
        data = self.kubectl_json(
            "-n", "educates-installer", "get", "secret", "educates-ingress-ca"
        )["data"]["ca.crt"]
        self.cafile = os.path.join(directory, f"{self.role}-ca.crt")
        with open(self.cafile, "wb") as stream:
            stream.write(base64.b64decode(data))


class PinnedHTTPSConnection(http.client.HTTPSConnection):
    """An HTTPS connection to a host name served on a fixed address, as a
    local cluster's ingress is served on its listen address only."""

    def __init__(self, host, address, context, timeout=60):
        super().__init__(host, 443, context=context, timeout=timeout)
        self._address = address

    def connect(self):
        sock = socket.create_connection((self._address, self.port), self.timeout)
        self.sock = self._context.wrap_socket(sock, server_hostname=self.host)


def https_request(cluster, url, method="GET", headers=None, body=None):
    parsed = urlsplit(url)
    context = ssl.create_default_context(cafile=cluster.cafile)
    connection = PinnedHTTPSConnection(parsed[0], cluster.listen_address, context)
    try:
        connection.request(method, parsed[1], body=body, headers=headers or {})
        response = connection.getresponse()
        return (
            response.status,
            response.getheader("Content-Type", ""),
            response.read(),
        )
    finally:
        connection.close()


def urlsplit(url):
    match = re.match(r"^https://([^/]+)(/.*)?$", url)
    if not match:
        die(f"not an https URL: {url}")
    return match.group(1), match.group(2) or "/"


def b64url_json(segment):
    padded = segment + "=" * (-len(segment) % 4)
    return json.loads(base64.urlsafe_b64decode(padded))


class Capture:
    def __init__(self, args):
        self.args = args
        self.cli = args.cli
        self.a = Cluster(self.cli, args.lookup_cluster, "cluster-a")
        self.b = Cluster(self.cli, args.remote_cluster, "cluster-b")
        self.work = tempfile.mkdtemp(prefix="lookup-golden-")
        self.a.write_ca(self.work)
        self.b.write_ca(self.work)
        self.remote_server = (
            args.remote_server
            or f"https://educates-{args.remote_cluster}-control-plane:6443"
        )

        self.out = os.path.abspath(args.out)
        self.fixtures_dir = os.path.join(self.out, "fixtures")
        self.states_dir = os.path.join(self.out, "states")
        for directory in (self.fixtures_dir, self.states_dir):
            shutil.rmtree(directory, ignore_errors=True)
            os.makedirs(directory)

        self.seq = 0
        self.step = None
        self.pending_actions = []
        # Literal values and the placeholders fixtures record for them.
        self.placeholders = {}
        # Tokens by placeholder name, as <token:NAME>.
        self.tokens = {}
        self.session_labels = {}
        self.session_count = {}
        self.lookup_host = None
        self.fixtures = []
        self.states = []

    # Placeholders -------------------------------------------------------

    def bind(self, literal, placeholder):
        if literal:
            self.placeholders[literal] = placeholder

    def refresh_uids(self):
        """Binds the uid of every object a response or token can show."""

        for cluster in (self.a, self.b):
            for kind in ("trainingportals", "workshopenvironments"):
                listing = cluster.kubectl_json("get", f"{kind}.training.educates.dev")
                for item in listing["items"]:
                    meta = item["metadata"]
                    name = kind_name(kind)
                    self.bind(
                        meta["uid"],
                        f"<uid:{cluster.role}/{name}/{meta['name']}>",
                    )
        listing = self.a.kubectl_json(
            "-n", CONFIG_NAMESPACE, "get", "clientconfigs.lookup.educates.dev"
        )
        for item in listing["items"]:
            meta = item["metadata"]
            if meta["uid"] not in self.placeholders:
                generation = sum(
                    1
                    for value in self.placeholders.values()
                    if value.startswith(f"<uid:cluster-a/ClientConfig/{meta['name']}")
                )
                suffix = f"#{generation + 1}" if generation else ""
                self.bind(
                    meta["uid"], f"<uid:cluster-a/ClientConfig/{meta['name']}{suffix}>"
                )

    def label_session(self, name, label):
        if name and name not in self.session_labels:
            self.session_labels[name] = label
            self.bind(name, f"<session:{label}>")

    def refresh_sessions(self):
        """Labels every session not yet labelled, by its environment and the
        order it was first seen in."""

        for cluster in (self.a, self.b):
            listing = cluster.kubectl_json("get", "workshopsessions.training.educates.dev")
            items = sorted(
                listing["items"], key=lambda item: item["metadata"]["creationTimestamp"]
            )
            for item in items:
                name = item["metadata"]["name"]
                environment = item["metadata"]["labels"].get(
                    "training.educates.dev/environment.name", ""
                )
                if name not in self.session_labels:
                    count = self.session_count.get(environment, 0) + 1
                    self.session_count[environment] = count
                    self.label_session(name, f"{environment}-{count}")
                # A session requested without a user id belongs to a user the
                # portal names with a random UUID.
                user = item.get("status", {}).get("educates", {}).get("user") or ""
                if UUID_PATTERN.match(user) and user not in self.placeholders:
                    self.bind(user, f"<portal-user:{self.session_labels[name]}>")

    def normalize_string(self, value):
        if value in self.placeholders:
            return self.placeholders[value]
        for token_name, token in self.tokens.items():
            if value == token:
                return token_name
        if JWT_PATTERN.match(value):
            return "<jwt>"
        return value

    def normalize_url(self, value):
        value = ACTIVATION_TOKEN_PATTERN.sub("token=<activation-token>", value)
        for literal, placeholder in self.placeholders.items():
            if literal in value:
                value = value.replace(literal, placeholder)
        return value

    def normalize_json(self, value, key=None):
        if isinstance(value, dict):
            return {k: self.normalize_json(v, k) for k, v in value.items()}
        if isinstance(value, list):
            return [self.normalize_json(v, key) for v in value]
        if key == "expires_at" and isinstance(value, int):
            return "<expires-at>"
        if key == "generation" and isinstance(value, int):
            return "<generation>"
        if key == "sessionActivationUrl" and isinstance(value, str):
            return self.normalize_url(value)
        if isinstance(value, str):
            return self.normalize_string(value)
        return value

    def normalize_kubeconfig(self, text):
        lines = []
        for line in text.splitlines():
            line = re.sub(r"^(\s*token: ).*$", r"\1<token>", line)
            line = re.sub(
                r"^(\s*certificate-authority-data: ).*$",
                r"\1<certificate-authority-data>",
                line,
            )
            line = line.replace(self.remote_server, "<server:cluster-b>")
            lines.append(line)
        return "\n".join(lines) + ("\n" if text.endswith("\n") else "")

    # Requests -----------------------------------------------------------

    def lookup(self, method, path, authorization=None, body=None, raw=False):
        headers = {}
        if authorization is not None:
            headers["Authorization"] = self.expand(authorization)
        data = None
        if body is not None:
            data = body.encode("utf-8") if isinstance(body, str) else body
            headers["Content-Type"] = "application/json"
        status, content_type, payload = https_request(
            self.a, f"https://{self.lookup_host}{path}", method, headers, data
        )
        if raw:
            return status, content_type, payload
        return status, content_type, payload.decode("utf-8")

    def expand(self, header):
        for token_name, token in self.tokens.items():
            header = header.replace(token_name, token)
        return header

    def capture(
        self,
        slug,
        description,
        method,
        path,
        authorization=None,
        body=None,
        body_format=None,
        issues=None,
        session_label=None,
        change=None,
        expect=None,
    ):
        """Sends one request and records its fixture, which finish() writes
        once every placeholder is known. issues names the token a login
        response issues, as <token:NAME>; session_label labels the session a
        session request returns."""

        if isinstance(body, (dict, list)):
            body = json.dumps(body)
        sent_at = int(time.time())
        status, content_type, text = self.lookup(method, path, authorization, body)

        claims = None
        parsed = None
        if content_type.startswith("application/json"):
            parsed = json.loads(text)
            if issues and status == 200:
                self.tokens[issues] = parsed["access_token"]
                self.refresh_uids()
                claims = self.check_token(parsed, sent_at)
            if session_label and status == 200 and parsed.get("sessionName"):
                self.label_session(parsed["sessionName"], session_label)
            if method == "POST" and path == "/api/v1/workshops":
                self.refresh_sessions()

        self.seq += 1
        fixture_id = f"{self.seq:03d}-{slug}"
        request = {"method": method, "path": path, "headers": {}}
        if authorization is not None:
            request["headers"]["Authorization"] = authorization
        if body is not None:
            request["headers"]["Content-Type"] = "application/json"
        request["body"] = body

        if parsed is not None:
            response_format, response_body = "json", parsed
        elif body_format == "yaml":
            response_format, response_body = "yaml", text
        else:
            response_format, response_body = "text", text

        fixture = {
            "id": fixture_id,
            "step": self.step,
            "description": description,
            "source": "captured",
            "actions": self.pending_actions,
            "request": request,
            "response": {
                "status": status,
                "contentType": content_type,
                "bodyFormat": response_format,
                "body": response_body,
            },
        }
        if claims is not None:
            fixture["issuedToken"] = {"name": issues, **claims}
        if change is not None:
            fixture["change"] = change
        self.pending_actions = []
        self.fixtures.append(fixture)
        # The raw record, tokens included, stays in the work directory only.
        with open(os.path.join(self.work, "raw.jsonl"), "a") as stream:
            stream.write(json.dumps(fixture) + "\n")
        log(f"{fixture_id}: {method} {path} -> {status}")

        if expect is not None and status != expect:
            die(f"{slug}: expected status {expect}, got {status}: {text}")
        return status, parsed if parsed is not None else text

    def check_token(self, response, sent_at):
        token = response["access_token"]
        header = b64url_json(token.split(".")[0])
        claims = b64url_json(token.split(".")[1])
        if header.get("alg") != "HS256":
            die(f"token header {header} is not HS256")
        if claims["exp"] != response["expires_at"]:
            die("the token's exp differs from expires_at")
        lifetime = response["expires_at"] - sent_at
        if abs(lifetime - TOKEN_LIFETIME) > 120:
            die(f"the token lasts {lifetime} seconds, not 72 hours")
        return {
            "header": header,
            "claims": {"sub": claims["sub"], "jti": claims["jti"], "exp": claims["exp"]},
            "lifetimeSeconds": TOKEN_LIFETIME,
        }

    def finish(self):
        """Writes every fixture and state, normalized with the placeholders
        the whole capture bound."""

        # The polling token is never a fixture's. A login in the same second
        # with the same claims issues the same token, which must take the
        # name of the fixture that issued it.
        self.tokens.pop("<token:poll>", None)
        for fixture in self.fixtures:
            response = fixture["response"]
            if response["bodyFormat"] == "json":
                response["body"] = self.normalize_json(response["body"])
            elif response["bodyFormat"] == "yaml":
                response["body"] = self.normalize_kubeconfig(response["body"])
            issued = fixture.get("issuedToken")
            if issued:
                issued["claims"]["jti"] = self.normalize_url(issued["claims"]["jti"])
                issued["claims"]["exp"] = "<expires-at>"
            if fixture["request"]["body"]:
                fixture["request"]["body"] = self.normalize_url(fixture["request"]["body"])
            fixture["actions"] = [self.normalize_url(a) for a in fixture["actions"]]
            path = os.path.join(self.fixtures_dir, f"{fixture['id']}.json")
            with open(path, "w") as stream:
                json.dump(fixture, stream, indent=2)
                stream.write("\n")
        for state in self.states:
            path = os.path.join(self.states_dir, f"{state['step']}.json")
            with open(path, "w") as stream:
                json.dump(self.normalize_state(state), stream, indent=2)
                stream.write("\n")

    def action(self, text):
        self.pending_actions.append(text)

    # Waiting ------------------------------------------------------------

    def wait_for(self, what, predicate, timeout=300, interval=2):
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                if predicate():
                    return True
            except (OSError, ValueError, KeyError, http.client.HTTPException):
                pass
            time.sleep(interval)
        return False

    def must_wait_for(self, what, predicate, timeout=300, interval=2):
        log(f"Waiting for {what}")
        if not self.wait_for(what, predicate, timeout, interval):
            die(f"timed out waiting for {what}")

    def admin_get(self, path):
        status, _, text = self.lookup("GET", path, "Bearer <token:poll>")
        if status != 200:
            raise ValueError(f"{path} answered {status}")
        return json.loads(text)

    def poll_login(self):
        status, _, text = self.lookup(
            "POST",
            "/auth/login",
            body=json.dumps({"username": ADMIN[0], "password": ADMIN[1]}),
        )
        if status != 200:
            raise ValueError(f"login answered {status}")
        self.tokens["<token:poll>"] = json.loads(text)["access_token"]
        return True

    def lookup_sessions(self, cluster, portal, environment):
        return self.admin_get(
            f"/api/v1/clusters/{cluster}/portals/{portal}/environments/{environment}/sessions"
        )["sessions"]

    def wait_session_phase(self, cluster, portal, environment, name, phase, timeout=180):
        def seen():
            for session in self.lookup_sessions(cluster, portal, environment):
                if session["name"] == name:
                    return session["phase"] == phase
            return False

        return self.wait_for(f"session {name} in phase {phase}", seen, timeout, 1)

    def activate(self, cluster, response):
        url = response["sessionActivationUrl"]
        status, _, _ = https_request(cluster, url)
        if status not in (200, 302):
            die(f"activating {response['sessionName']} answered {status}")
        self.action(
            f"activate session <session:{self.session_labels[response['sessionName']]}>"
        )

    # States -------------------------------------------------------------

    def snapshot(self, step, description):
        self.step = step
        self.refresh_uids()
        self.refresh_sessions()
        state = {"step": step, "description": description, "clusters": {}}
        for cluster in (self.a, self.b):
            state["clusters"][cluster.role] = self.project(cluster)
        self.states.append(state)
        log(f"State {step}")

    def project(self, cluster):
        portals = []
        for item in cluster.kubectl_json("get", "trainingportals.training.educates.dev")["items"]:
            spec = item.get("spec", {}).get("portal", {})
            status = item.get("status", {}).get("educates", {})
            portals.append(
                {
                    "name": item["metadata"]["name"],
                    "uid": item["metadata"]["uid"],
                    "generation": item["metadata"]["generation"],
                    "spec": {
                        "portal": {
                            "labels": spec.get("labels", []),
                            "sessions": {"maximum": spec.get("sessions", {}).get("maximum")},
                        }
                    },
                    "status": {
                        "educates": {
                            "phase": status.get("phase"),
                            "url": status.get("url"),
                            "namespace": status.get("namespace"),
                            "credentialSecrets": {
                                "robot": status.get("credentialSecrets", {}).get("robot")
                            },
                        }
                    },
                }
            )
        environments = []
        for item in cluster.kubectl_json("get", "workshopenvironments.training.educates.dev")["items"]:
            status = item.get("status", {}).get("educates", {})
            workshop = status.get("workshop", {})
            workshop_spec = workshop.get("spec", {})
            environments.append(
                {
                    "name": item["metadata"]["name"],
                    "uid": item["metadata"]["uid"],
                    "labels": item["metadata"].get("labels", {}),
                    "spec": {"workshop": {"name": item["spec"]["workshop"]["name"]}},
                    "status": {
                        "educates": {
                            "phase": status.get("phase"),
                            "capacity": status.get("capacity"),
                            "reserved": status.get("reserved"),
                            "workshop": {
                                "generation": workshop.get("generation"),
                                "spec": {
                                    "title": workshop_spec.get("title"),
                                    "description": workshop_spec.get("description"),
                                    "labels": workshop_spec.get("labels", []),
                                },
                            },
                        }
                    },
                }
            )
        sessions = []
        for item in cluster.kubectl_json("get", "workshopsessions.training.educates.dev")["items"]:
            status = item.get("status", {}).get("educates", {})
            labels = item["metadata"].get("labels", {})
            sessions.append(
                {
                    "name": item["metadata"]["name"],
                    "generation": item["metadata"]["generation"],
                    "labels": {
                        key: labels[key]
                        for key in sorted(labels)
                        if key.startswith("training.educates.dev/")
                    },
                    "spec": {"workshop": {"name": item["spec"]["workshop"]["name"]}},
                    "status": {
                        "educates": {"phase": status.get("phase"), "user": status.get("user")}
                    },
                }
            )
        sessions.sort(key=lambda item: self.session_labels.get(item["name"], item["name"]))
        return {
            "trainingPortals": portals,
            "workshopEnvironments": environments,
            "workshopSessions": sessions,
        }

    def normalize_state(self, value):
        if isinstance(value, dict):
            return {k: self.normalize_state(v) for k, v in value.items()}
        if isinstance(value, list):
            return [self.normalize_state(v) for v in value]
        if isinstance(value, str) and value in self.placeholders:
            return self.placeholders[value]
        return value

    # The scenario -------------------------------------------------------

    def setup(self):
        scenario = os.path.join(SCRIPT_DIR, "scenario")
        log("Applying the training resources")
        self.a.kubectl("apply", "-f", os.path.join(scenario, "cluster-a", "training.yaml"))
        self.b.kubectl("apply", "-f", os.path.join(scenario, "cluster-b", "training.yaml"))

        def portals_ready(cluster, environments):
            listing = cluster.kubectl_json("get", "workshopenvironments.training.educates.dev")
            phases = [
                item.get("status", {}).get("educates", {}).get("phase")
                for item in listing["items"]
            ]
            return len(phases) == environments and all(p == "Running" for p in phases)

        self.must_wait_for("cluster-a's environments", lambda: portals_ready(self.a, 2), 600)
        self.must_wait_for("cluster-b's environments", lambda: portals_ready(self.b, 1), 600)

        def reserved_available():
            listing = self.a.kubectl_json("get", "workshopsessions.training.educates.dev")
            return any(
                item.get("status", {}).get("educates", {}).get("phase") == "Available"
                for item in listing["items"]
            )

        self.must_wait_for("portal-a's reserved session", reserved_available, 600)

        log("Writing cluster-b's kubeconfig Secret")
        kubeconfig = run(
            [self.cli, "admin", "lookup", "kubeconfig", "--local-cluster", self.b.local_name]
        ).stdout
        kubeconfig = re.sub(
            r"(?m)^(\s*server: ).*$", lambda m: m.group(1) + self.remote_server, kubeconfig
        )
        path = os.path.join(self.work, "cluster-b-kubeconfig.yaml")
        with open(path, "w") as stream:
            stream.write(kubeconfig)
        secret = self.a.kubectl(
            "-n", CONFIG_NAMESPACE, "create", "secret", "generic", "cluster-b-kubeconfig",
            f"--from-file=config={path}", "--dry-run=client", "-o", "yaml",
        ).stdout
        self.a.kubectl("apply", "-f", "-", input_text=secret)

        log("Applying the lookup configuration")
        for name in ("clusterconfigs.yaml", "tenantconfigs.yaml", "clientconfigs.yaml"):
            self.a.kubectl("apply", "-f", os.path.join(scenario, "lookup", name))

        url = self.a.kubectl_json("get", "lookupservices.platform.educates.dev", "cluster")[
            "status"
        ]["url"]
        self.lookup_host = urlsplit(url)[0]

        self.must_wait_for("the Lookup Service to log in its admin client", self.poll_login)
        self.wait_for_inventory()

    def wait_for_inventory(self):
        def complete():
            portals = {
                (p["cluster"], p["name"]): p for p in self.admin_get("/api/v1/portals")["portals"]
            }
            if set(portals) != {("cluster-a", "portal-a"), ("cluster-b", "portal-b")}:
                return False
            for cluster, portal, count in (("cluster-a", "portal-a", 2), ("cluster-b", "portal-b", 1)):
                if portals[(cluster, portal)]["phase"] != "Running":
                    return False
                environments = self.admin_get(
                    f"/api/v1/clusters/{cluster}/portals/{portal}/environments"
                )["environments"]
                if len(environments) != count or any(e["phase"] != "Running" for e in environments):
                    return False
            return any(
                s["phase"] == "Available"
                for s in self.lookup_sessions("cluster-a", "portal-a", "portal-a-w01")
            )

        self.must_wait_for("the Lookup Service to see both clusters' portals", complete, 300)

    def mint_token(self, subject, jti, expires_in, key=None):
        """Signs a token in the Lookup Service's pod: with the service's own
        key, as the service signs them, or with another key."""

        program = (
            "import sys, time, jwt\n"
            "from service.config import jwt_token_secret\n"
            "key = sys.argv[4] or jwt_token_secret()\n"
            "print(jwt.encode({'sub': sys.argv[1], 'jti': sys.argv[2],"
            " 'exp': int(time.time()) + int(sys.argv[3])}, key, algorithm='HS256'))\n"
        )
        return self.a.kubectl(
            "-n", SERVICE_NAMESPACE, "exec", "deploy/lookup-service", "--",
            "python", "-c", program, subject, jti, str(expires_in), key or "",
        ).stdout.strip()

    def jti_of(self, token_name):
        return b64url_json(self.tokens[token_name].split(".")[1])["jti"]

    def run(self):
        self.setup()
        self.step_initial()
        self.step_sessions()
        self.step_client_changes()
        self.step_label_without_value()
        self.step_cluster_deleted()
        self.step_restarted()
        self.finish()
        self.write_provenance()
        log(f"Wrote {self.seq} fixtures to {self.fixtures_dir}")

    def step_initial(self):
        for item in self.a.kubectl_json("get", "workshopsessions.training.educates.dev")["items"]:
            if item.get("status", {}).get("educates", {}).get("phase") == "Available":
                self.label_session(item["metadata"]["name"], "alpha-reserved")
        self.snapshot(
            "initial",
            "Both portals run, cluster-a's lab-alpha holds one reserved session, "
            "and no session has been requested.",
        )
        c = self.capture
        T = "Bearer <token:tenant-client>"
        A = "Bearer <token:admin-client>"
        B = "Bearer <token:both-client>"

        # Logging in.
        c("auth-login", "A tenant client logs in.", "POST", "/auth/login",
          body={"username": TENANT[0], "password": TENANT[1]},
          issues="<token:tenant-client>", expect=200)
        c("login-alias", "An admin client logs in through the deprecated /login alias.",
          "POST", "/login", body={"username": ADMIN[0], "password": ADMIN[1]},
          issues="<token:admin-client>", expect=200)
        for client, issues in ((BOTH, "both-client"), (USER, "user-client"),
                               (REVOKE, "revoke-client"), (ROTATE, "rotate-client"),
                               (DOOMED, "doomed-client")):
            c(f"auth-login-{client[0]}", f"{client[0]} logs in.", "POST", "/auth/login",
              body={"username": client[0], "password": client[1]},
              issues=f"<token:{issues}>", expect=200)
        c("auth-login-missing-username", "A login without a username.", "POST",
          "/auth/login", body={"password": TENANT[1]})
        c("auth-login-missing-password", "A login without a password.", "POST",
          "/auth/login", body={"username": TENANT[0]})
        c("auth-login-wrong-password", "A login with the wrong password.", "POST",
          "/auth/login", body={"username": TENANT[0], "password": "not-the-password"})
        c("auth-login-unknown-client", "A login as a client that does not exist.",
          "POST", "/auth/login", body={"username": "nobody", "password": "not-the-password"})
        c("login-alias-wrong-password", "A login through /login with the wrong password.",
          "POST", "/login", body={"username": TENANT[0], "password": "not-the-password"})
        c("auth-login-malformed-json", "A login whose body is not valid JSON.", "POST",
          "/auth/login", body='{"username": "tenant-client", "password": ',
          change=change_malformed_json("/auth/login"))
        c("auth-login-empty-body", "A login with an empty JSON body.", "POST",
          "/auth/login", body="", change=change_malformed_json("/auth/login"))
        c("auth-login-array-body", "A login whose JSON body is an array, not an object.",
          "POST", "/auth/login", body="[]", change=change_malformed_json("/auth/login"))

        # Verifying tokens, and what the token middleware refuses.
        expired = self.mint_token(TENANT[0], self.jti_of("<token:tenant-client>"), -3600)
        foreign = self.mint_token(
            TENANT[0], self.jti_of("<token:tenant-client>"), 3600, key="not-the-service-key"
        )
        self.tokens["<token:expired>"] = expired
        self.tokens["<token:foreign>"] = foreign
        c("auth-verify", "A valid token is verified.", "GET", "/auth/verify", T)
        c("auth-verify-admin", "An admin client's token is verified.", "GET",
          "/auth/verify", A)
        c("auth-verify-no-token", "A verify without a token.", "GET", "/auth/verify")
        c("auth-verify-bearer-without-token", "An Authorization header of one word.",
          "GET", "/auth/verify", "Bearer")
        c("auth-verify-basic-scheme", "An Authorization header with the Basic scheme.",
          "GET", "/auth/verify", "Basic dGVuYW50LWNsaWVudDpwYXNzd29yZA==")
        c("auth-verify-three-words", "An Authorization header of three words.",
          "GET", "/auth/verify", "Bearer one two")
        c("auth-verify-not-a-jwt", "A bearer token that is not a JWT.", "GET",
          "/auth/verify", "Bearer not-a-jwt")
        c("auth-verify-expired", "A token whose exp has passed, signed with the "
          "service's key in its pod, with the client's current jti.", "GET",
          "/auth/verify", "Bearer <token:expired>")
        c("auth-verify-foreign-key", "A token signed with another key, as a token "
          "issued before a restart of the Python service is.", "GET", "/auth/verify",
          "Bearer <token:foreign>")
        c("unknown-route", "A path no route serves.", "GET", "/api/v1/unknown", A)
        c("auth-login-wrong-method", "A GET on the login route.", "GET", "/auth/login")

        # Clients.
        c("clients-list", "The admin lists the clients.", "GET", "/api/v1/clients", A)
        c("clients-list-tenant-role", "A tenant client lists the clients.", "GET",
          "/api/v1/clients", T)
        c("clients-list-both-roles", "A client with both roles lists the clients.",
          "GET", "/api/v1/clients", B)
        c("clients-list-no-token", "The client list without a token.", "GET",
          "/api/v1/clients")
        c("clients-get-self", "A tenant client reads its own details.", "GET",
          "/api/v1/clients/tenant-client", T)
        c("clients-get-other-tenant-role", "A tenant client reads another client.",
          "GET", "/api/v1/clients/admin-client", T)
        c("clients-get-other-both-roles", "A client with both roles reads another "
          "client: the tenant role's restriction applies.", "GET",
          "/api/v1/clients/tenant-client", B)
        c("clients-get-user-client", "The admin reads a client that sets spec.user.",
          "GET", "/api/v1/clients/user-client", A)
        c("clients-get-unknown", "The admin reads a client that does not exist.",
          "GET", "/api/v1/clients/nobody", A)

        # Tenants.
        c("tenants-list", "The admin lists the tenants.", "GET", "/api/v1/tenants", A)
        c("tenants-list-tenant-role", "A tenant client lists the tenants.", "GET",
          "/api/v1/tenants", T)
        c("tenants-get", "The admin reads a tenant.", "GET", "/api/v1/tenants/tenant-all", A)
        c("tenants-get-unknown", "The admin reads a tenant that does not exist.", "GET",
          "/api/v1/tenants/tenant-unknown", A)
        for tenant in ("tenant-all", "tenant-names", "tenant-labels", "tenant-none"):
            c(f"tenants-portals-{tenant}", f"The admin lists {tenant}'s portals.", "GET",
              f"/api/v1/tenants/{tenant}/portals", A)
        c("tenants-portals-unknown", "The admin lists the portals of a tenant that "
          "does not exist.", "GET", "/api/v1/tenants/tenant-unknown/portals", A)
        c("tenants-portals-tenant-role", "A tenant client lists a tenant's portals.",
          "GET", "/api/v1/tenants/tenant-all/portals", T)

        # Portals and clusters.
        c("portals-list", "The admin lists every portal.", "GET", "/api/v1/portals", A)
        c("portals-list-tenant-role", "A tenant client lists every portal.", "GET",
          "/api/v1/portals", T)
        c("clusters-list", "The admin lists the clusters.", "GET", "/api/v1/clusters", A)
        c("clusters-list-tenant-role", "A tenant client lists the clusters.", "GET",
          "/api/v1/clusters", T)
        c("clusters-get", "The admin reads cluster-a.", "GET", "/api/v1/clusters/cluster-a", A)
        c("clusters-get-remote", "The admin reads cluster-b.", "GET",
          "/api/v1/clusters/cluster-b", A)
        c("clusters-get-unknown", "The admin reads a cluster that does not exist.",
          "GET", "/api/v1/clusters/nowhere", A)
        c("clusters-kubeconfig-local", "The admin reads the local cluster's kubeconfig, "
          "made from the mounted remote-access token.", "GET",
          "/api/v1/clusters/cluster-a/kubeconfig", A, body_format="yaml")
        c("clusters-kubeconfig-remote", "The admin reads the remote cluster's "
          "kubeconfig, reduced to its one context.", "GET",
          "/api/v1/clusters/cluster-b/kubeconfig", A, body_format="yaml")
        c("clusters-kubeconfig-unknown", "The kubeconfig of a cluster that does not "
          "exist.", "GET", "/api/v1/clusters/nowhere/kubeconfig", A)
        c("clusters-kubeconfig-tenant-role", "A tenant client reads a kubeconfig.",
          "GET", "/api/v1/clusters/cluster-a/kubeconfig", T)
        self.capture_inventory("initial", A)

        # Workshop lists.
        for tenant in ("tenant-all", "tenant-names", "tenant-labels", "tenant-none"):
            c(f"workshops-list-{tenant}", f"A tenant client lists {tenant}'s workshops.",
              "GET", f"/api/v1/workshops?tenant={tenant}", T)
        c("workshops-list-tenant-role-no-tenant", "A tenant client lists workshops "
          "without a tenant.", "GET", "/api/v1/workshops", T)
        c("workshops-list-tenant-not-allowed", "A tenant client lists the workshops of "
          "a tenant its patterns do not match.", "GET",
          "/api/v1/workshops?tenant=other-tenant", T)
        c("workshops-list-tenant-unknown", "A tenant client lists the workshops of a "
          "tenant that does not exist.", "GET", "/api/v1/workshops?tenant=tenant-unknown", T)
        c("workshops-list-admin-no-tenant", "The admin lists every cluster's workshops.",
          "GET", "/api/v1/workshops", A)
        c("workshops-list-admin-tenant", "The admin lists a tenant's workshops.", "GET",
          "/api/v1/workshops?tenant=tenant-names", A)
        c("workshops-list-admin-tenant-unknown", "The admin lists the workshops of a "
          "tenant that does not exist.", "GET", "/api/v1/workshops?tenant=tenant-unknown", A)
        c("workshops-list-both-roles-no-tenant", "A client with both roles lists "
          "workshops without a tenant.", "GET", "/api/v1/workshops", B)
        c("workshops-list-both-roles-tenant-not-allowed", "A client with both roles "
          "lists the workshops of a tenant its patterns do not match.", "GET",
          "/api/v1/workshops?tenant=tenant-names", B)
        c("workshops-list-no-token", "The workshop list without a token.", "GET",
          "/api/v1/workshops?tenant=tenant-all")
        for tenant in ("tenant-all", "tenant-names", "tenant-labels", "tenant-none"):
            c(f"tenant-workshops-{tenant}", f"A tenant client lists {tenant}'s workshops "
              "by the tenant route.", "GET", f"/api/v1/tenants/{tenant}/workshops", T)
        c("tenant-workshops-not-allowed", "A tenant client lists, by the tenant route, "
          "the workshops of a tenant its patterns do not match.", "GET",
          "/api/v1/tenants/other-tenant/workshops", T)
        c("tenant-workshops-unknown", "A tenant client lists, by the tenant route, the "
          "workshops of a tenant that does not exist.", "GET",
          "/api/v1/tenants/tenant-unknown/workshops", T)
        c("tenant-workshops-admin", "The admin lists a tenant's workshops by the tenant "
          "route.", "GET", "/api/v1/tenants/tenant-labels/workshops", A)

        # Session requests that are refused before any session is made.
        def request(**fields):
            body = {"clientIndexUrl": INDEX_URL}
            body.update(fields)
            return {k: v for k, v in body.items() if v is not None}

        c("session-missing-tenant", "A session request without tenantName.", "POST",
          "/api/v1/workshops", T, request(workshopName="lab-alpha"))
        c("session-missing-workshop", "A session request without workshopName.", "POST",
          "/api/v1/workshops", T, request(tenantName="tenant-all"))
        c("session-tenant-not-allowed", "A session request for a tenant the client's "
          "patterns do not match.", "POST", "/api/v1/workshops", T,
          request(tenantName="other-tenant", workshopName="lab-alpha"))
        c("session-tenant-unknown", "A session request for a tenant that does not "
          "exist.", "POST", "/api/v1/workshops", T,
          request(tenantName="tenant-unknown", workshopName="lab-alpha"))
        c("session-tenant-without-portals", "A session request for a tenant that "
          "reaches no portal.", "POST", "/api/v1/workshops", T,
          request(tenantName="tenant-none", workshopName="lab-alpha"))
        c("session-workshop-unknown", "A session request for a workshop no portal "
          "offers.", "POST", "/api/v1/workshops", T,
          request(tenantName="tenant-all", workshopName="lab-gamma"))
        c("session-workshop-not-on-tenant", "A session request for a workshop the "
          "tenant's portals do not offer.", "POST", "/api/v1/workshops", T,
          request(tenantName="tenant-names", workshopName="lab-beta"))
        c("session-admin-without-tenants", "The admin, whose ClientConfig names no "
          "tenants, requests a session.", "POST", "/api/v1/workshops", A,
          request(tenantName="tenant-all", workshopName="lab-alpha"))
        c("session-missing-index-url", "A session request without clientIndexUrl: "
          "every candidate portal refuses it.", "POST", "/api/v1/workshops", T,
          {"tenantName": "tenant-all", "workshopName": "lab-alpha"},
          change={
              "summary": "A request without clientIndexUrl answers 400 with a "
              "plain-text body naming the field, before any portal is contacted.",
              "spec": "REST API contract: the changes; user story 18",
              "expected": {
                  "status": 400,
                  "contentType": "text/plain; charset=utf-8",
                  "bodyFormat": "text",
                  "body": "Missing clientIndexUrl",
                  "bodyContains": "clientIndexUrl",
              },
          })
        c("session-malformed-json", "A session request whose body is not valid JSON.",
          "POST", "/api/v1/workshops", T, '{"tenantName": "tenant-all", ',
          change=change_malformed_json("/api/v1/workshops"))
        c("session-array-body", "A session request whose JSON body is an array.",
          "POST", "/api/v1/workshops", T, "[]",
          change=change_malformed_json("/api/v1/workshops"))
        c("session-no-token", "A session request without a token.", "POST",
          "/api/v1/workshops", None, request(tenantName="tenant-all", workshopName="lab-alpha"))

    def capture_inventory(self, step, authorization, which=("a", "b")):
        """Captures the admin inventory routes of both clusters' portals."""

        c = self.capture
        if "a" in which:
            c(f"{step}-cluster-a-portals", "The admin lists cluster-a's portals.", "GET",
              "/api/v1/clusters/cluster-a/portals", authorization)
            c(f"{step}-portal-a", "The admin reads portal-a.", "GET",
              "/api/v1/clusters/cluster-a/portals/portal-a", authorization)
            c(f"{step}-portal-a-environments", "The admin lists portal-a's environments.",
              "GET", "/api/v1/clusters/cluster-a/portals/portal-a/environments", authorization)
            for env in ("portal-a-w01", "portal-a-w02"):
                base = f"/api/v1/clusters/cluster-a/portals/portal-a/environments/{env}"
                c(f"{step}-{env}", f"The admin reads environment {env}.", "GET", base,
                  authorization)
                c(f"{step}-{env}-sessions", f"The admin lists {env}'s sessions.", "GET",
                  f"{base}/sessions", authorization)
                c(f"{step}-{env}-users", f"The admin lists {env}'s users.", "GET",
                  f"{base}/users", authorization)
        if "b" in which:
            c(f"{step}-cluster-b-portals", "The admin lists cluster-b's portals.", "GET",
              "/api/v1/clusters/cluster-b/portals", authorization)
            c(f"{step}-portal-b", "The admin reads portal-b.", "GET",
              "/api/v1/clusters/cluster-b/portals/portal-b", authorization)
            c(f"{step}-portal-b-environments", "The admin lists portal-b's environments.",
              "GET", "/api/v1/clusters/cluster-b/portals/portal-b/environments", authorization)
            base = "/api/v1/clusters/cluster-b/portals/portal-b/environments/portal-b-w01"
            c(f"{step}-portal-b-w01", "The admin reads environment portal-b-w01.", "GET",
              base, authorization)
            c(f"{step}-portal-b-w01-sessions", "The admin lists portal-b-w01's sessions.",
              "GET", f"{base}/sessions", authorization)
            c(f"{step}-portal-b-w01-users", "The admin lists portal-b-w01's users.", "GET",
              f"{base}/users", authorization)
        if step == "initial":
            base = "/api/v1/clusters/cluster-a/portals"
            c("cluster-portals-unknown-cluster", "The portals of a cluster that does not "
              "exist.", "GET", "/api/v1/clusters/nowhere/portals", authorization)
            c("portal-unknown", "A portal that does not exist.", "GET",
              f"{base}/portal-z", authorization)
            c("portal-unknown-cluster", "A portal of a cluster that does not exist.",
              "GET", "/api/v1/clusters/nowhere/portals/portal-a", authorization)
            c("environments-unknown-portal", "The environments of a portal that does not "
              "exist.", "GET", f"{base}/portal-z/environments", authorization)
            c("environment-unknown", "An environment that does not exist.", "GET",
              f"{base}/portal-a/environments/portal-a-w99", authorization)
            c("environment-sessions-unknown", "The sessions of an environment that does "
              "not exist.", "GET", f"{base}/portal-a/environments/portal-a-w99/sessions",
              authorization)
            c("environment-users-unknown", "The users of an environment that does not "
              "exist.", "GET", f"{base}/portal-a/environments/portal-a-w99/users",
              authorization)
            c("environment-user-sessions-unknown", "A user's sessions in an environment "
              "that does not exist.", "GET",
              f"{base}/portal-a/environments/portal-a-w99/users/user-1/sessions",
              authorization)
            c("environment-user-sessions-none", "The sessions of a user who has none.",
              "GET", f"{base}/portal-a/environments/portal-a-w01/users/user-1/sessions",
              authorization)
            c("portal-tenant-role", "A tenant client reads a portal.", "GET",
              f"{base}/portal-a", "Bearer <token:tenant-client>")

    def step_sessions(self):
        c = self.capture
        T = "Bearer <token:tenant-client>"
        A = "Bearer <token:admin-client>"
        U = "Bearer <token:user-client>"
        beta_user_1 = {
            "tenantName": "tenant-all",
            "workshopName": "lab-beta",
            "clientUserId": "user-1",
            "clientActionId": "action-1",
            "clientIndexUrl": INDEX_URL,
            "userEmailAddress": "user-1@example.test",
            "userFirstName": "User",
            "userLastName": "One",
            "workshopParams": [{"name": "greeting", "value": "hello"}],
        }
        _, first = c("session-request", "A tenant client requests lab-beta, which only "
                     "portal-a offers, for user-1.", "POST", "/api/v1/workshops", T,
                     beta_user_1, session_label="beta-user-1", expect=200)
        session = first["sessionName"]
        if not self.wait_session_phase("cluster-a", "portal-a", "portal-a-w02", session,
                                       "Allocating", 30):
            die("the Lookup Service did not see the requested session as Allocating")
        self.snapshot("beta-allocating", "user-1's lab-beta session on portal-a waits "
                      "for activation in phase Allocating.")
        base = "/api/v1/clusters/cluster-a/portals/portal-a"
        env = f"{base}/environments/portal-a-w02"
        counted = (
            "Counting and ordering: an environment's and a portal's allocated count "
            "include sessions in the Allocating phase"
        )
        c("allocating-portal-a", "The admin reads portal-a while a session is "
          "Allocating.", "GET", base, A,
          change=change_count(counted, "allocated", 1))
        c("allocating-portal-a-w02", "The admin reads portal-a-w02 while its session "
          "is Allocating.", "GET", env, A, change=change_count(counted, "allocated", 1))
        c("allocating-portal-a-w02-sessions", "The admin lists portal-a-w02's sessions.",
          "GET", f"{env}/sessions", A)
        c("allocating-portal-a-w02-users", "The admin lists portal-a-w02's users.",
          "GET", f"{env}/users", A)
        c("allocating-portal-a-w02-user-sessions", "The admin lists user-1's sessions "
          "in portal-a-w02.", "GET", f"{env}/users/user-1/sessions", A)
        c("session-reacquire-allocating", "The same request again, while the session "
          "is Allocating, returns the same session.", "POST", "/api/v1/workshops", T,
          beta_user_1, expect=200)

        self.activate(self.a, first)
        if not self.wait_session_phase("cluster-a", "portal-a", "portal-a-w02", session,
                                       "Allocated", 60):
            die("the Lookup Service did not see the activated session as Allocated")
        self.snapshot("beta-allocated", "user-1 has activated the lab-beta session, "
                      "which is now Allocated.")
        c("allocated-portal-a", "The admin reads portal-a once its session is "
          "Allocated.", "GET", base, A)
        c("allocated-portal-a-w02", "The admin reads portal-a-w02 once its session is "
          "Allocated.", "GET", env, A)
        c("allocated-portal-a-w02-sessions", "The admin lists portal-a-w02's sessions.",
          "GET", f"{env}/sessions", A)
        c("session-reacquire-allocated", "The same request again, once the session is "
          "Allocated, returns the same session.", "POST", "/api/v1/workshops", T,
          beta_user_1, expect=200)
        c("session-affinity-outside-tenant", "user-1 requests lab-beta through "
          "tenant-names, whose only portal, portal-b, does not offer it: the Python "
          "service returns user-1's session on portal-a, outside the tenant's reach.",
          "POST", "/api/v1/workshops", T,
          dict(beta_user_1, tenantName="tenant-names"),
          change={
              "summary": "A returning user is matched only against sessions on portals "
              "the requesting tenant can reach. tenant-names reaches no portal that "
              "offers lab-beta, so the request finds no workshop.",
              "spec": "Session affinity; user story 27",
              "expected": {"status": 503, "contentType": "text/plain; charset=utf-8",
                           "bodyFormat": "text", "body": "Workshop not available"},
          })

        _, remote = c("session-request-remote", "A tenant client requests lab-alpha for "
                      "user-2 through tenant-names, which reaches only portal-b on the "
                      "remote cluster.", "POST", "/api/v1/workshops", T,
                      {"tenantName": "tenant-names", "workshopName": "lab-alpha",
                       "clientUserId": "user-2", "clientIndexUrl": INDEX_URL},
                      session_label="alpha-user-2", expect=200)
        remote_session = remote["sessionName"]
        remote_requested = time.time()
        if not self.wait_session_phase("cluster-b", "portal-b", "portal-b-w01",
                                       remote_session, "Allocating", 30):
            die("the Lookup Service did not see user-2's session as Allocating")
        self.snapshot("remote-allocating", "user-2's lab-alpha session on portal-b, on "
                      "the remote cluster, waits for activation in phase Allocating; "
                      "user-1's session on portal-a is Allocated.")
        counted = (
            "Counting and ordering: an environment's and a portal's allocated count "
            "include sessions in the Allocating phase"
        )
        rb = "/api/v1/clusters/cluster-b/portals/portal-b/environments/portal-b-w01"
        c("remote-allocating-portal-b", "The admin reads portal-b while user-2's "
          "session is Allocating.", "GET", "/api/v1/clusters/cluster-b/portals/portal-b",
          A, change=change_count(counted, "allocated", 1))
        c("remote-allocating-portal-b-w01", "The admin reads portal-b-w01 while "
          "user-2's session is Allocating.", "GET", rb, A,
          change=change_count(counted, "allocated", 1))
        c("remote-allocating-portal-b-w01-sessions", "The admin lists portal-b-w01's "
          "sessions.", "GET", f"{rb}/sessions", A)
        c("remote-allocating-portal-b-w01-users", "The admin lists portal-b-w01's "
          "users.", "GET", f"{rb}/users", A)
        c("remote-allocating-user-2-sessions", "The admin lists user-2's sessions in "
          "portal-b-w01.", "GET", f"{rb}/users/user-2/sessions", A)
        c("remote-allocating-tenant-all-portals", "The admin lists tenant-all's "
          "portals.", "GET", "/api/v1/tenants/tenant-all/portals", A,
          change={
              "summary": "portal-b's allocated count includes user-2's Allocating "
              "session; portal-a's, whose one session is Allocated, is the same.",
              "spec": counted,
              "expected": {"status": 200,
                           "items": {"portals": {"cluster-b/portal-b": {"allocated": 1}}}},
          })
        c("session-reacquire-remote", "user-2 requests lab-alpha through tenant-names "
          "again while the remote session is Allocating, and gets it back.", "POST",
          "/api/v1/workshops", T,
          {"tenantName": "tenant-names", "workshopName": "lab-alpha",
           "clientUserId": "user-2", "clientIndexUrl": INDEX_URL}, expect=200)
        # Reacquiring a session that waits for activation restarts the
        # portal's 60 second activation window.
        remote_requested = time.time()

        _, fixed = c("session-request-spec-user", "A client whose ClientConfig sets "
                     "spec.user requests lab-alpha through tenant-all with another "
                     "clientUserId: spec.user wins, and portal-a's reserved session is "
                     "handed out.", "POST", "/api/v1/workshops", U,
                     {"tenantName": "tenant-all", "workshopName": "lab-alpha",
                      "clientUserId": "someone-else", "clientIndexUrl": INDEX_URL},
                     expect=200)
        if self.session_labels.get(fixed["sessionName"]) != "alpha-reserved":
            log(f"note: spec.user's session is {fixed['sessionName']}, not the reserved one")
        self.activate(self.a, fixed)

        _, anonymous = c("session-request-no-user", "A tenant client requests lab-beta "
                         "without clientUserId.", "POST", "/api/v1/workshops", T,
                         {"tenantName": "tenant-all", "workshopName": "lab-beta",
                          "clientIndexUrl": INDEX_URL},
                         session_label="beta-anonymous", expect=200)
        self.activate(self.a, anonymous)
        for name, environment in ((fixed["sessionName"], "portal-a-w01"),
                                  (anonymous["sessionName"], "portal-a-w02")):
            if not self.wait_session_phase("cluster-a", "portal-a", environment, name,
                                           "Allocated", 60):
                die(f"the Lookup Service did not see {name} as Allocated")
        self.snapshot("portal-a-busy", "portal-a's lab-beta is at its capacity of 2 "
                      "with two Allocated sessions, and lab-alpha has the spec.user "
                      "session Allocated and a new reserved one.")
        self.capture_inventory("busy", A, which=("a",))
        c("session-environment-full", "A request for lab-beta, whose only environment "
          "is at its capacity: portal-a refuses it.", "POST", "/api/v1/workshops", T,
          {"tenantName": "tenant-all", "workshopName": "lab-beta",
           "clientUserId": "user-3", "clientIndexUrl": INDEX_URL})

        # user-2 never activates the remote session. 60 seconds after the last
        # request for it the portal expires it and deletes its WorkshopSession,
        # which stays in phase Allocating until it is gone: the portal sets
        # the Stopping phase only on reserved sessions it terminates and on the
        # sessions of an environment that stops.
        def remote_gone():
            names = [s["name"] for s in self.lookup_sessions("cluster-b", "portal-b",
                                                              "portal-b-w01")]
            return remote_session not in names

        log("Waiting for user-2's unactivated session to expire")
        time.sleep(max(0, 60 - (time.time() - remote_requested)))
        self.must_wait_for("user-2's expired session to be deleted", remote_gone, 300)
        self.action("wait for user-2's unactivated session on portal-b to expire, 60 "
                    "seconds after it was last requested, and for its WorkshopSession "
                    "to be deleted")
        self.snapshot("remote-expired", "user-2's unactivated session on portal-b has "
                      "expired and its WorkshopSession is gone.")
        _, again = c("session-after-expiry", "user-2 requests lab-alpha through "
                     "tenant-names again once the earlier session expired: a new "
                     "session is created.", "POST", "/api/v1/workshops", T,
                     {"tenantName": "tenant-names", "workshopName": "lab-alpha",
                      "clientUserId": "user-2", "clientIndexUrl": INDEX_URL},
                     session_label="alpha-user-2-again", expect=200)
        self.activate(self.b, again)
        if not self.wait_session_phase("cluster-b", "portal-b", "portal-b-w01",
                                       again["sessionName"], "Allocated", 60):
            die("the Lookup Service did not see user-2's new session as Allocated")

        # The portal terminates portal-a's reserved lab-alpha session, putting
        # it in phase Stopping, once the portal's session maximum falls below
        # its sessions. Lowering the workshop's reserved count does not, while
        # the environment keeps the initial count it was created with.
        reserved = [s["name"] for s in self.lookup_sessions("cluster-a", "portal-a",
                                                             "portal-a-w01")
                    if s["phase"] == "Available"]
        if len(reserved) != 1:
            die(f"portal-a-w01 has {len(reserved)} reserved sessions, not 1")
        self.a.kubectl(
            "patch", "trainingportals.training.educates.dev", "portal-a", "--type=json",
            "-p", json.dumps([
                {"op": "replace", "path": "/spec/portal/sessions/maximum", "value": 3},
            ]),
        )
        self.action("update TrainingPortal portal-a on cluster-a: spec.portal.sessions."
                    "maximum becomes 3, one less than its sessions, so the portal "
                    "terminates its reserved lab-alpha session")
        stopping = self.wait_session_phase("cluster-a", "portal-a", "portal-a-w01",
                                           reserved[0], "Stopping", 120)
        if stopping:
            self.step = "reserved-stopping"
            stop_counted = (
                "Counting and ordering: an environment's and a portal's allocated count "
                "include sessions in the Stopping phase"
            )
            env = "/api/v1/clusters/cluster-a/portals/portal-a/environments/portal-a-w01"
            c("stopping-portal-a-w01-sessions", "The admin lists portal-a-w01's sessions "
              "while the portal terminates its reserved session.", "GET",
              f"{env}/sessions", A)
            c("stopping-portal-a-w01", "The admin reads portal-a-w01 while its reserved "
              "session is Stopping.", "GET", env, A,
              change=change_count(stop_counted, "allocated", 2))
            c("stopping-portal-a", "The admin reads portal-a while a reserved session "
              "is Stopping.", "GET", "/api/v1/clusters/cluster-a/portals/portal-a", A,
              change=change_count(stop_counted, "allocated", 4))
            self.snapshot("reserved-stopping", "The portal has put portal-a's reserved "
                          "lab-alpha session in phase Stopping, terminating it; taken "
                          "right after the requests of this step.")
        else:
            log("note: the reserved session was not seen Stopping; its fixtures are "
                "skipped")

        def reserved_gone():
            names = [s["name"] for s in self.lookup_sessions("cluster-a", "portal-a",
                                                              "portal-a-w01")]
            return reserved[0] not in names

        self.must_wait_for("the terminated reserved session to be deleted",
                           reserved_gone, 300)
        self.action("wait for the terminated reserved session's WorkshopSession to be "
                    "deleted")

    def step_client_changes(self):
        c = self.capture
        self.snapshot("sessions-settled", "Every session left is Allocated: user-1's "
                      "and the anonymous user's on lab-beta and the spec.user session "
                      "on lab-alpha on portal-a, now at its maximum of 3, and "
                      "user-2's second session on portal-b.")
        self.capture_inventory("settled", "Bearer <token:admin-client>")
        c("settled-user-1-sessions", "The admin lists user-1's sessions in "
          "portal-a-w02.", "GET",
          "/api/v1/clusters/cluster-a/portals/portal-a/environments/portal-a-w02/users/"
          "user-1/sessions", "Bearer <token:admin-client>")
        c("settled-fixed-user-sessions", "The admin lists fixed-user's sessions in "
          "portal-a-w01.", "GET",
          "/api/v1/clusters/cluster-a/portals/portal-a/environments/portal-a-w01/users/"
          "fixed-user/sessions", "Bearer <token:admin-client>")

        R = "Bearer <token:revoke-client>"
        c("auth-logout", "revoke-client logs out.", "POST", "/auth/logout", R, expect=200)
        c("auth-verify-revoked", "revoke-client's token after its logout.", "GET",
          "/auth/verify", R)
        c("auth-logout-revoked", "A logout with the revoked token.", "POST",
          "/auth/logout", R)
        c("auth-logout-no-token", "A logout without a token.", "POST", "/auth/logout")
        c("auth-logout-not-a-jwt", "A logout with a bearer token that is not a JWT.",
          "POST", "/auth/logout", "Bearer not-a-jwt")
        c("clients-get-revoked", "revoke-client reads its own details with the revoked "
          "token.", "GET", "/api/v1/clients/revoke-client", R)

        self.a.kubectl(
            "-n", CONFIG_NAMESPACE, "patch", "clientconfigs.lookup.educates.dev",
            "revoke-client", "--type=merge", "-p",
            json.dumps({"spec": {"tenants": ["tenant-all", "tenant-names"]}}),
        )
        self.action("update ClientConfig revoke-client: spec.tenants becomes "
                     "[tenant-all, tenant-names], its password unchanged")
        self.must_wait_for(
            "the Lookup Service to see revoke-client's new tenants",
            lambda: self.admin_get("/api/v1/clients/revoke-client")["tenants"]
            == ["tenant-all", "tenant-names"],
        )
        c("auth-verify-revoked-after-update", "revoke-client's revoked token after an "
          "update of its ClientConfig that keeps its password.", "GET", "/auth/verify",
          R, change=change_unauthorized(
              "An update to a ClientConfig that does not change its password leaves "
              "its revoked tokens revoked.", "Tokens; user story 8",
              "Client identity does not match"))

        O = "Bearer <token:rotate-client>"
        self.a.kubectl(
            "-n", CONFIG_NAMESPACE, "patch", "clientconfigs.lookup.educates.dev",
            "rotate-client", "--type=merge", "-p",
            json.dumps({"spec": {"client": {"password": ROTATED_PASSWORD}}}),
        )
        self.action(f"update ClientConfig rotate-client: spec.client.password becomes "
                    f"{ROTATED_PASSWORD}")

        def rotated():
            status, _, _ = self.lookup("POST", "/auth/login", body=json.dumps(
                {"username": ROTATE[0], "password": ROTATED_PASSWORD}))
            return status == 200

        self.must_wait_for("the Lookup Service to see rotate-client's new password", rotated)
        c("auth-verify-after-password-change", "rotate-client's token issued before its "
          "password changed.", "GET", "/auth/verify", O,
          change=change_unauthorized(
              "Changing a client's password revokes the tokens issued to it.",
              "Tokens; user story 9", "Client identity does not match"))
        c("auth-login-old-password", "rotate-client logs in with its old password.",
          "POST", "/auth/login", body={"username": ROTATE[0], "password": ROTATE[1]})
        c("auth-login-new-password", "rotate-client logs in with its new password.",
          "POST", "/auth/login", body={"username": ROTATE[0], "password": ROTATED_PASSWORD},
          issues="<token:rotate-client#2>", expect=200)

        D = "Bearer <token:doomed-client>"
        self.a.kubectl("-n", CONFIG_NAMESPACE, "delete", "clientconfigs.lookup.educates.dev",
                       "doomed-client")
        self.action("delete ClientConfig doomed-client")
        self.must_wait_for(
            "the Lookup Service to forget doomed-client",
            lambda: "doomed-client"
            not in [x["name"] for x in self.admin_get("/api/v1/clients")["clients"]],
        )
        c("auth-verify-deleted-client", "doomed-client's token once its ClientConfig is "
          "deleted.", "GET", "/auth/verify", D)
        c("auth-logout-deleted-client", "doomed-client logs out once its ClientConfig is "
          "deleted.", "POST", "/auth/logout", D)
        c("auth-login-deleted-client", "doomed-client logs in once its ClientConfig is "
          "deleted.", "POST", "/auth/login",
          body={"username": DOOMED[0], "password": DOOMED[1]})

        manifest = json.loads(self.a.kubectl(
            "apply", "--dry-run=client", "-o", "json", "-f",
            os.path.join(SCRIPT_DIR, "scenario", "lookup", "clientconfigs.yaml"),
        ).stdout)
        doomed = next(i for i in manifest["items"] if i["metadata"]["name"] == "doomed-client")
        self.a.kubectl("apply", "-f", "-", input_text=json.dumps(doomed))
        self.action("create ClientConfig doomed-client again, from scenario/lookup/"
                    "clientconfigs.yaml, with a new uid")
        self.must_wait_for(
            "the Lookup Service to see doomed-client again",
            lambda: "doomed-client"
            in [x["name"] for x in self.admin_get("/api/v1/clients")["clients"]],
        )
        c("auth-verify-recreated-client", "The first doomed-client's token once the "
          "ClientConfig is created again.", "GET", "/auth/verify", D)
        c("auth-login-recreated-client", "The new doomed-client logs in.", "POST",
          "/auth/login", body={"username": DOOMED[0], "password": DOOMED[1]},
          issues="<token:doomed-client#2>", expect=200)
        c("auth-verify-recreated-client-new-token", "The new doomed-client's token.",
          "GET", "/auth/verify", "Bearer <token:doomed-client#2>")

    def step_label_without_value(self):
        c = self.capture
        A = "Bearer <token:admin-client>"
        T = "Bearer <token:tenant-client>"
        self.b.kubectl(
            "patch", "trainingportals.training.educates.dev", "portal-b", "--type=json",
            "-p", json.dumps([{"op": "add", "path": "/spec/portal/labels/-",
                               "value": {"name": "featured"}}]),
        )
        self.action("update TrainingPortal portal-b on cluster-b: add the label "
                    "featured, without a value, to spec.portal.labels")

        def seen():
            labels = self.admin_get("/api/v1/clusters/cluster-b/portals/portal-b")["labels"]
            return {"name": "featured"} in labels

        self.must_wait_for("the Lookup Service to see portal-b's featured label", seen)
        self.snapshot("label-without-value", "portal-b has a label without a value.")
        crash = {
            "summary": "A label without a value matches the empty string, so tenant "
            "matching works and the request answers as it would without the label.",
            "spec": "Tenant matching: a label without a value has the empty string as "
            "its value; user story 39",
        }
        c("label-portal-b", "The admin reads portal-b, which has a label without a "
          "value.", "GET", "/api/v1/clusters/cluster-b/portals/portal-b", A)
        c("label-cluster-b-portals", "The admin lists cluster-b's portals, which needs "
          "no tenant matching.", "GET", "/api/v1/clusters/cluster-b/portals", A)
        c("label-workshops-admin", "The admin lists every workshop, which needs no "
          "tenant matching.", "GET", "/api/v1/workshops", A)
        c("label-workshops-tenant-all", "A tenant client lists tenant-all's workshops, "
          "which needs portal-b's labels.", "GET", "/api/v1/workshops?tenant=tenant-all",
          T, change=dict(crash, expected={"status": 200, "contentType":
                                          "application/json; charset=utf-8",
                                          "bodyFormat": "json",
                                          "sameBodyAs": "label-workshops-admin"}))
        c("label-tenant-portals-tenant-names", "The admin lists tenant-names' portals, "
          "which needs portal-b's labels.", "GET", "/api/v1/tenants/tenant-names/portals",
          A, change=dict(crash, expected={"status": 200, "contentType":
                                          "application/json; charset=utf-8",
                                          "bodyFormat": "json",
                                          "sameBodyAs": "label-cluster-b-portals"}))
        c("label-workshops-tenant-labels", "A tenant client lists tenant-labels' "
          "workshops, which never looks at cluster-b's portals.", "GET",
          "/api/v1/workshops?tenant=tenant-labels", T)

        self.b.kubectl(
            "patch", "trainingportals.training.educates.dev", "portal-b", "--type=json",
            "-p", json.dumps([{"op": "remove", "path": "/spec/portal/labels/1"}]),
        )
        self.action("update TrainingPortal portal-b on cluster-b: remove the featured "
                    "label again")
        self.must_wait_for("the Lookup Service to see portal-b without its featured label",
                           lambda: not seen())

    def step_cluster_deleted(self):
        c = self.capture
        A = "Bearer <token:admin-client>"
        T = "Bearer <token:tenant-client>"
        self.a.kubectl("-n", CONFIG_NAMESPACE, "delete", "clusterconfigs.lookup.educates.dev",
                       "cluster-b", "--wait=false")
        self.action("delete ClusterConfig cluster-b")

        def gone():
            names = [x["name"] for x in self.admin_get("/api/v1/clusters")["clusters"]]
            return "cluster-b" not in names

        removed = self.wait_for("cluster-b to leave the cluster list", gone, 60)
        self.action("wait up to 60 seconds for cluster-b to leave GET /api/v1/clusters"
                    + ("" if removed else "; it did not"))
        portals_listed = any(
            p["cluster"] == "cluster-b" for p in self.admin_get("/api/v1/portals")["portals"]
        )
        self.snapshot("cluster-b-deleted", "cluster-b's ClusterConfig is deleted; its "
                      "training resources are still there.")
        expected_clusters = {
            "summary": "A deleted ClusterConfig stops placement on its cluster at once "
            "and removes the cluster and its portals from every response (#701).",
            "spec": "Composition: cluster registry; user story 34",
        }
        c("deleted-clusters-list", "The admin lists the clusters once cluster-b's "
          "ClusterConfig is deleted.", "GET", "/api/v1/clusters", A,
          change=None if removed else dict(expected_clusters, expected={
              "status": 200, "only": {"clusters": ["cluster-a"]}}))
        c("deleted-cluster-get", "The admin reads cluster-b once its ClusterConfig is "
          "deleted.", "GET", "/api/v1/clusters/cluster-b", A,
          change=None if removed else dict(expected_clusters, expected={
              "status": 404, "contentType": "text/plain; charset=utf-8",
              "bodyFormat": "text", "body": "Cluster not available"}))
        c("deleted-portals-list", "The admin lists every portal once cluster-b's "
          "ClusterConfig is deleted.", "GET", "/api/v1/portals", A,
          change=None if not portals_listed else dict(expected_clusters, expected={
              "status": 200, "only": {"portals": ["cluster-a/portal-a"]}}))
        c("deleted-workshops-tenant-names", "A tenant client lists tenant-names' "
          "workshops, which only cluster-b offered.", "GET",
          "/api/v1/workshops?tenant=tenant-names", T,
          change=None if not portals_listed else dict(expected_clusters, expected={
              "status": 200, "contentType": "application/json; charset=utf-8",
              "bodyFormat": "json", "body": {"workshops": []}}))
        c("deleted-session-tenant-names", "A tenant client requests lab-alpha for user-4 "
          "through tenant-names, which only cluster-b served.", "POST",
          "/api/v1/workshops", T,
          {"tenantName": "tenant-names", "workshopName": "lab-alpha",
           "clientUserId": "user-4", "clientIndexUrl": INDEX_URL},
          session_label="alpha-user-4",
          change=None if not portals_listed else dict(expected_clusters, expected={
              "status": 503, "contentType": "text/plain; charset=utf-8",
              "bodyFormat": "text", "body": "Workshop not available"}))

    def step_restarted(self):
        c = self.capture
        T = "Bearer <token:tenant-client>"
        old_pod = self.a.kubectl(
            "-n", SERVICE_NAMESPACE, "get", "pods", "-l", "app=lookup-service", "-o",
            "jsonpath={.items[0].metadata.name}",
        ).stdout.strip()
        self.a.kubectl("-n", SERVICE_NAMESPACE, "delete", "pod", old_pod)
        self.action("restart the Lookup Service: delete its pod and wait for the new "
                    "one to be Ready and to list cluster-a's portal again")

        def ready():
            pods = self.a.kubectl_json("-n", SERVICE_NAMESPACE, "get", "pods", "-l",
                                       "app=lookup-service")["items"]
            return any(
                p["metadata"]["name"] != old_pod
                and any(s["type"] == "Ready" and s["status"] == "True"
                        for s in p["status"].get("conditions", []))
                for p in pods
            )

        self.must_wait_for("the new Lookup Service pod to be Ready", ready, 300)
        self.must_wait_for("the restarted Lookup Service to log in its admin client",
                           self.poll_login, 120)

        def portal_back():
            portals = self.admin_get("/api/v1/portals")["portals"]
            return any(p["name"] == "portal-a" and p["phase"] == "Running" for p in portals)

        self.must_wait_for("the restarted Lookup Service to see portal-a", portal_back, 300)
        self.snapshot("restarted", "The Lookup Service has restarted.")
        c("restarted-verify-old-token", "tenant-client's token issued before the "
          "restart.", "GET", "/auth/verify", T,
          change={
              "summary": "Tokens survive a restart: the signing key lives in a Secret "
              "the service owns, read on every start.",
              "spec": "Tokens; user story 4",
              "expected": {"status": 200, "contentType": "application/json; charset=utf-8",
                           "bodyFormat": "json", "body": {}},
          })
        c("restarted-login", "tenant-client logs in again after the restart.", "POST",
          "/auth/login", body={"username": TENANT[0], "password": TENANT[1]},
          issues="<token:tenant-client#2>", expect=200)
        c("restarted-verify-new-token", "tenant-client's new token.", "GET",
          "/auth/verify", "Bearer <token:tenant-client#2>")
        c("restarted-login-admin", "The admin logs in again after the restart.", "POST",
          "/auth/login", body={"username": ADMIN[0], "password": ADMIN[1]},
          issues="<token:admin-client#2>", expect=200)
        c("restarted-clusters-list", "The admin lists the clusters after the restart.",
          "GET", "/api/v1/clusters", "Bearer <token:admin-client#2>")

    def write_provenance(self):
        commit = run(["git", "-C", REPO_ROOT, "rev-parse", "HEAD"]).stdout.strip()
        source = run(["git", "-C", REPO_ROOT, "log", "-1", "--format=%H", "--",
                      "lookup-service/service", "lookup-service/pyproject.toml",
                      "lookup-service/uv.lock"]).stdout.strip()
        dirty = run(["git", "-C", REPO_ROOT, "status", "--porcelain", "--",
                     "lookup-service/service", "lookup-service/pyproject.toml",
                     "lookup-service/uv.lock"]).stdout.strip()
        pod = self.a.kubectl_json("-n", SERVICE_NAMESPACE, "get", "pods", "-l",
                                  "app=lookup-service")["items"][0]
        status = pod["status"]["containerStatuses"][0]
        version = self.a.kubectl_json("version")["serverVersion"]["gitVersion"]
        provenance = {
            "capturedAt": datetime.datetime.now(datetime.timezone.utc)
            .strftime("%Y-%m-%dT%H:%M:%SZ"),
            "commit": commit,
            "lookupServiceSourceCommit": source,
            "lookupServiceSourceClean": not dirty,
            "cliVersion": run([self.cli, "version"]).stderr.strip()
            or run([self.cli, "version"]).stdout.strip(),
            "image": status["image"],
            "imageID": status["imageID"],
            "kubernetesVersion": version,
            "clusters": {
                "cluster-a": {"localCluster": self.a.local_name, "domain": self.a.domain,
                              "lookupHost": self.lookup_host},
                "cluster-b": {"localCluster": self.b.local_name, "domain": self.b.domain,
                              "server": self.remote_server},
            },
            "fixtures": self.seq,
        }
        with open(os.path.join(self.out, "capture.json"), "w") as stream:
            json.dump(provenance, stream, indent=2)
            stream.write("\n")


def kind_name(resource):
    return {"trainingportals": "TrainingPortal",
            "workshopenvironments": "WorkshopEnvironment"}[resource]


def change_malformed_json(path):
    return {
        "summary": f"A body on {path} that is not a JSON object answers 400 instead of "
        "crashing the handler with a 500.",
        "spec": "REST API contract: the changes; user story 19",
        "expected": {
            "status": 400,
            "contentType": "text/plain; charset=utf-8",
            "bodyFormat": "text",
        },
    }


def change_count(spec, field, value):
    return {
        "summary": f"The {field} count includes the session the Python service does "
        "not count.",
        "spec": spec,
        "expected": {"status": 200, "fields": {field: value}},
    }


def change_unauthorized(summary, spec, body):
    return {
        "summary": summary,
        "spec": spec,
        "expected": {
            "status": 401,
            "contentType": "text/plain; charset=utf-8",
            "bodyFormat": "text",
            "body": body,
        },
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--lookup-cluster", required=True,
                        help="the named local cluster that runs the Lookup Service")
    parser.add_argument("--remote-cluster", required=True,
                        help="the named local cluster the Lookup Service monitors remotely")
    parser.add_argument("--remote-server", default=None,
                        help="the remote cluster's API server address, as the lookup "
                        "cluster's pods reach it (default: its kind control plane "
                        "container, https://educates-<name>-control-plane:6443)")
    parser.add_argument("--cli", default=os.path.join(
        REPO_ROOT, "client-programs", "bin", "educates-darwin-arm64"),
        help="the educates CLI of the build under test")
    parser.add_argument("--out", default=SCRIPT_DIR,
                        help="where to write fixtures/, states/ and capture.json")
    Capture(parser.parse_args()).run()


if __name__ == "__main__":
    main()
