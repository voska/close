#!/usr/bin/env python3
"""Exercise the real binary with synthetic HTTP state, never a live CRM."""
import json
import os
import subprocess
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse


class Handler(BaseHTTPRequestHandler):
    record = None
    methods = []

    def log_message(self, *_args):
        pass

    def respond(self, body, status=200):
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        if status != 204:
            self.wfile.write(json.dumps(body).encode())

    def do_GET(self):
        assert self.headers.get("Authorization") == "Basic Zml4dHVyZS1rZXk6"
        parsed = urlparse(self.path)
        if parsed.path == "/api/v1/lead/":
            skip = int(parse_qs(parsed.query).get("_skip", [0])[0])
            self.respond({"data": [{"id": f"lead_{skip}", "name": "Demo"}], "has_more": skip == 0})
        elif parsed.path == "/api/v1/lead/lead_demo/" and self.record:
            self.respond(self.record)
        elif parsed.path == "/api/v1/me/":
            self.respond({"id": "user_demo", "organization_id": "orga_demo"})
        else:
            self.respond({"error": "not found"}, 404)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if self.path == "/api/v1/data/search/":
            cursor = body.get("cursor")
            self.respond({"data": [{"id": "lead_1" if cursor else "lead_0"}], "cursor": None if cursor else "next"})
        else:
            type(self).methods.append("POST")
            type(self).record = {"id": "lead_demo", **body}
            self.respond({"id": "lead_demo"}, 201)

    def do_PUT(self):
        type(self).methods.append("PUT")
        type(self).record.update(json.loads(self.rfile.read(int(self.headers["Content-Length"]))))
        self.respond(self.record)

    def do_DELETE(self):
        type(self).methods.append("DELETE")
        type(self).record = None
        self.respond({}, 204)


def main():
    binary = os.path.abspath(sys.argv[1])
    env = {k: v for k, v in os.environ.items() if not k.startswith("CLOSE_")}
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    worker = threading.Thread(target=server.serve_forever)
    worker.start()
    env.update(CLOSE_BASE_URL=f"http://127.0.0.1:{server.server_port}/api/v1/", CLOSE_API_KEY="fixture-key")

    def run(*args, code=0):
        result = subprocess.run([binary, *args], env=env, capture_output=True, text=True, timeout=10)
        assert result.returncode == code, (args, result.returncode, result.stderr)
        if code:
            error = json.loads(result.stderr)["error"]
            assert error["code"] == code and error["type"] and error["message"]
        else:
            assert not result.stderr, result.stderr
        return json.loads(result.stdout) if result.stdout else None

    try:
        assert run("version")["version"]
        assert run("schema", "exit-codes")["exit_codes"]["9"] == "unverified"
        assert run("auth", "status")["verified"] is True
        assert len(run("lead", "list", "--all")["data"]) == 2
        assert len(run("search", "--data", '{"query":{"type":"object_type","object_type":"lead"}}', "--all")["data"]) == 2
        assert run("lead", "create", "--data", '{"name":"Demo"}')["name"] == "Demo"
        assert run("lead", "update", "lead_demo", "--data", '{"name":"Updated"}')["name"] == "Updated"
        assert run("lead", "delete", "lead_demo", "--confirm")["deleted"] is True
        run("lead", "delete", "lead_demo", code=2)
        assert run("lead", "create", "--data", '{"name":"Preview"}', "--dry-run")["dry_run"] is True
        assert Handler.methods == ["POST", "PUT", "DELETE"]
        run("api", "POST", "lead/", "--data", "{}", "--confirm", code=9)
        run("--readonly", "lead", "create", "--data", "{}", code=2)
        run("lead", "get", "../me", code=2)
        print("Binary smoke: version, schema, auth, pagination, search, verified CRUD, dry-run, raw-write and error contracts passed")
    finally:
        server.shutdown()
        worker.join()
        server.server_close()


if __name__ == "__main__":
    main()
