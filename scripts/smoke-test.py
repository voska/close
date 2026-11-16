#!/usr/bin/env python3
"""Exercise the real binary with synthetic HTTP state, never a live CRM."""
import json
import os
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse


class Handler(BaseHTTPRequestHandler):
    record = None
    task_record = None
    methods = []
    verify_status = 200

    def log_message(self, *_args):
        pass

    def respond(self, body, status=200):
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        if status == 503:
            self.send_header("Retry-After", "31")
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
        elif parsed.path == "/api/v1/task/task_demo/":
            if self.task_record is not None:
                self.respond(self.task_record)
            else:
                self.respond({"error": "_fields is not supported"}, self.verify_status)
        elif parsed.path == "/api/v1/me/":
            self.respond({"id": "user_demo", "organization_id": "orga_demo"})
        else:
            self.respond({"error": "not found"}, 404)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if self.path == "/api/v1/task/":
            type(self).methods.append("POST")
            type(self).task_record = {"id": "task_demo", **body}
            self.respond(self.task_record)
        elif self.path == "/api/v1/data/search/":
            cursor = body.get("cursor")
            self.respond({"data": [{"id": "lead_1" if cursor else "lead_0"}], "cursor": None if cursor else "next"})
        else:
            type(self).methods.append("POST")
            type(self).record = {"id": "lead_demo", **body}
            self.respond({"id": "lead_demo"}, 201)

    def do_PUT(self):
        type(self).methods.append("PUT")
        if self.path == "/api/v1/task/task_demo/":
            self.rfile.read(int(self.headers["Content-Length"]))
            self.respond({"id": "task_demo", "is_complete": False, "text": "SECRET_SMS_BODY", "body_preview": "SECRET_EMAIL_BODY"})
            return
        type(self).record.update(json.loads(self.rfile.read(int(self.headers["Content-Length"]))))
        self.respond(self.record)

    def do_DELETE(self):
        type(self).methods.append("DELETE")
        type(self).record = None
        self.respond({}, 204)



class PolicyHandler(Handler):
    requests = []
    verify_status = 200
    echo = {"id": "task_demo", "text": "SECRET_SMS_BODY", "body_preview": "SECRET_EMAIL_BODY",
            "attachments": ["SECRET_MMS"], "tasks": [{"text": "SECRET_EMBEDDED_TASK"}]}

    def do_GET(self):
        parsed = urlparse(self.path)
        query = parse_qs(parsed.query, keep_blank_values=True)
        self.requests.append(["GET", parsed.path, query])
        if self.verify_status == "timeout":
            time.sleep(0.5)
            return
        if self.verify_status != 200:
            self.respond({"error": "_fields is not supported"}, self.verify_status)
            return
        record = {"id": "task_demo", "lead_id": "lead_x", "text": "Call back", "date": "2026-11-15"}
        fields = query.get("_fields", [""])[0].split(",")
        record = {k: v for k, v in record.items() if k in fields}
        if parsed.path == "/api/v1/task/":
            self.respond({"data": [] if query.get("id") == ["task_sms"] else [record]})
        else:
            self.respond(record)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.requests.append([self.command, self.path, body])
        if self.path == "/api/v1/data/search/":
            self.respond({"data": []})
        else:
            id_value = "task_sms" if self.path.endswith("/task_sms/") else "task_demo"
            self.respond({**body, **self.echo, "id": id_value})

    do_PUT = do_POST


def policy_checks(run):
    assert os.getuid() == 65534
    assert run("task", "list", "--filter", "_type=lead", "--api-fields", "id,lead_id,text,date")["data"][0]["text"] == "Call back"
    refusals = [
        ["task", "list", "--filter", "_type=incoming_sms", "--api-fields", "id,text"],
        ["task", "list", "--api-fields", "id,text"],
        ["task", "list", "--filter", "_type=lead", "--filter", "_type__in=lead,incoming_sms", "--api-fields", "id,text"],
        ["task", "list", "--filter", "_type=lead", "--filter", "_type=lead", "--api-fields", "id,text"],
        ["task", "get", "task_demo", "--api-fields", "id,text"],
        *[["task", "list", "--api-fields", "id," + field] for field in ("remote_phone", "body_preview", "made_up")],
        ["search", "--data", '{"_fields":{"task.incoming_sms":["text"]}}'],
        ["search", "--data", '{"_fields":{"task.voicemail":["remote_phone"]}}'],
    ]
    for args in refusals:
        PolicyHandler.requests.clear()
        assert run(*args, code=11) is None
        assert not PolicyHandler.requests, PolicyHandler.requests
    run("search", "--data", '{"_fields":{"task.lead":["text"]}}')
    PolicyHandler.requests.clear()
    observed = run("task", "create", "--data", '{"lead_id":"lead_x","text":"Call back"}', "--confirm")
    assert observed == {"id": "task_demo", "lead_id": "lead_x", "text": "Call back"}
    assert PolicyHandler.requests == [
        ["POST", "/api/v1/task/", {"lead_id": "lead_x", "text": "Call back"}],
        ["GET", "/api/v1/task/", {"id": ["task_demo"], "_type": ["lead"], "_fields": ["id,lead_id,text"]}],
    ], PolicyHandler.requests
    print("Pinned task verification:", json.dumps(PolicyHandler.requests))
    PolicyHandler.requests.clear()
    assert run("task", "update", "task_sms", "--data", '{"text":"x"}', "--confirm", code=9) == {"id": "task_sms"}
    assert PolicyHandler.requests[-1] == ["GET", "/api/v1/task/", {"id": ["task_sms"], "_type": ["lead"], "_fields": ["id,text"]}]
    assert len(PolicyHandler.requests) == 2
    for resource, body in (("task", '{"is_complete":false}'), ("lead", '{"name":"Demo"}'), ("task", '{"text":"x"}')):
        for status in (400, 503, "timeout"):
            PolicyHandler.verify_status = status
            PolicyHandler.requests.clear()
            timeout = ("--timeout", "200ms") if status == "timeout" else ()
            assert run(resource, "update", "task_demo", "--data", body, "--confirm", *timeout, code=9) == {"id": "task_demo"}
            assert len(PolicyHandler.requests) == 2, PolicyHandler.requests
            assert "_fields" in PolicyHandler.requests[1][2]
        PolicyHandler.verify_status = 200
    PolicyHandler.requests.clear()
    assert run("task", "update", "task_demo", "--data", '{"text":"x","remote_phone":"x"}', "--confirm", code=9) == {"id": "task_demo"}
    assert len(PolicyHandler.requests) == 1
    for key in ("", " x", "x,"):
        PolicyHandler.requests.clear()
        assert run("task", "update", "task_demo", "--data", json.dumps({key: 0}), "--confirm", code=2) is None
        assert not PolicyHandler.requests
    print("Linux loopback policy smoke: allowlist, pinned text reads/writes, id-only failures and invalid keys passed as uid 65534")


def main():
    binary = os.path.abspath(sys.argv[1])
    env = {k: v for k, v in os.environ.items() if not k.startswith("CLOSE_")}
    policy_mode = "--policy" in sys.argv
    server = ThreadingHTTPServer(("127.0.0.1", 0), PolicyHandler if policy_mode else Handler)
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
        if policy_mode:
            print(json.dumps({"args": args, "exit": result.returncode, "stdout": result.stdout.strip()}))
        return json.loads(result.stdout) if result.stdout else None

    try:
        if policy_mode:
            policy_checks(run)
            return
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
        for key in ("", " ", "x,", " name", "x y"):
            assert run("task", "update", "task_demo", "--data", json.dumps({"is_complete": False, key: 0}), code=2) is None
        assert Handler.methods == ["POST", "PUT", "DELETE"]
        for status in (400, 503):
            Handler.verify_status = status
            assert run("task", "update", "task_demo", "--data", '{"is_complete":false}', code=9) == {"id": "task_demo"}
        assert run("task", "create", "--data", '{"lead_id":"lead_x","text":"Call back"}') == {"id": "task_demo", "lead_id": "lead_x", "text": "Call back"}
        assert run("task", "get", "task_demo")["text"] == "Call back"
        run("api", "POST", "lead/", "--data", "{}", "--confirm", code=9)
        run("--readonly", "lead", "create", "--data", "{}", code=2)
        run("lead", "get", "../me", code=2)
        print("Binary smoke: version, schema, auth, pagination, search, verified CRUD, dry-run, raw-write, narrowed unverified echoes and error contracts passed")
    finally:
        server.shutdown()
        worker.join()
        server.server_close()


if __name__ == "__main__":
    main()
