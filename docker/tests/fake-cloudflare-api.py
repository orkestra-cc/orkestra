#!/usr/bin/env python3
"""Fake Cloudflare API v4 for docker/tests/cloudflare-tunnel.test.sh.

Serves only what `./orkestra.sh tunnel` calls: zone lookup by name, DNS
records listed by name, and the batch endpoint
POST /zones/{zone}/dns_records/batch {deletes, posts}, applied like
Cloudflare does: deletes first, then posts, all or nothing. Single-record
POST/DELETE exist too (the switch must not need them). State lives in a JSON
file the test seeds and inspects:

  {"token": "...", "zones": {"example.com": "zone-1"},
   "records": [{"id", "type", "name", "content", "proxied", "ttl"}...],
   "fail_batch": ["host.example.com"],  # any batch touching that name fails
   "lax_name": false}                   # list by name ALSO returns wildcards

Every request is appended to <state>.log as "METHOD PATH AUTH-OK|AUTH-BAD";
every batch body to <state>.batches (one JSON per line). A wrong bearer token
gets the API's 403 shape. Listens on 127.0.0.1 on a free port and writes it to
<state>.port. Never talks to the network.
"""
import copy
import json
import os
import sys
import threading
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

STATE = sys.argv[1]
LOCK = threading.Lock()


def load():
    with open(STATE) as f:
        return json.load(f)


def save(state):
    with open(STATE + ".tmp", "w") as f:
        json.dump(state, f, indent=1)
    os.replace(STATE + ".tmp", STATE)


class BatchError(Exception):
    pass


def create(records, body):
    """Add one record to `records`, or raise like the API would."""
    if body.get("type") not in ("A", "AAAA", "CNAME", "TXT", "MX"):
        raise BatchError("unsupported record type")
    clash = [r for r in records if r["name"] == body.get("name")
             and r["type"] in ("A", "AAAA", "CNAME")
             and (r["type"] == "CNAME" or body.get("type") == "CNAME")]
    if clash:
        raise BatchError("a CNAME cannot coexist with another record of the same name")
    record = {k: body.get(k) for k in ("type", "name", "content", "proxied", "ttl", "comment", "tags")}
    record = {k: v for k, v in record.items() if v is not None}
    record["id"] = uuid.uuid4().hex
    records.append(record)
    return record


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, code, body):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def ok(self, result):
        self.reply(200, {"success": True, "errors": [], "result": result})

    def fail(self, code, message):
        self.reply(code, {"success": False, "errors": [{"code": code, "message": message}], "result": None})

    def handle_any(self, method):
        with LOCK:
            state = load()
            auth_ok = self.headers.get("Authorization") == "Bearer " + state["token"]
            url = urlparse(self.path)
            with open(STATE + ".log", "a") as log:
                log.write(f"{method} {url.path}{'?' + url.query if url.query else ''} {'AUTH-OK' if auth_ok else 'AUTH-BAD'}\n")
            if not auth_ok:
                return self.fail(403, "Authentication error")
            parts = url.path.strip("/").split("/")
            query = parse_qs(url.query)
            body = {}
            length = int(self.headers.get("Content-Length") or 0)
            if length:
                body = json.loads(self.rfile.read(length))

            if parts == ["zones"] and method == "GET":
                name = query.get("name", [""])[0]
                zid = state["zones"].get(name)
                return self.ok([{"id": zid, "name": name}] if zid else [])
            if len(parts) < 3 or parts[0] != "zones" or parts[2] != "dns_records":
                return self.fail(404, "not implemented by the fake")
            if parts[1] not in state["zones"].values():
                return self.fail(404, "zone not found")

            if len(parts) == 3 and method == "GET":
                name = query.get("name", [None])[0]
                lax = state.get("lax_name", False)
                return self.ok([r for r in state["records"] if name is None or r["name"] == name
                                or (lax and r["name"].startswith("*."))])

            if len(parts) == 4 and parts[3] == "batch" and method == "POST":
                with open(STATE + ".batches", "a") as log:
                    log.write(json.dumps(body) + "\n")
                records = copy.deepcopy(state["records"])
                try:
                    touched = set()
                    for d in body.get("deletes") or []:
                        match = [r for r in records if r["id"] == d.get("id")]
                        if not match:
                            raise BatchError(f"record {d.get('id')} not found")
                        touched.add(match[0]["name"])
                        records.remove(match[0])
                    for p in body.get("patches") or []:
                        raise BatchError("patches not implemented by the fake")
                    for p in body.get("puts") or []:
                        raise BatchError("puts not implemented by the fake")
                    created = []
                    for p in body.get("posts") or []:
                        if str(p.get("name", "")).startswith("*."):
                            raise BatchError("the fake refuses wildcard writes (the switch must never send one)")
                        touched.add(p.get("name"))
                        created.append(create(records, p))
                    if touched & set(state.get("fail_batch", [])):
                        raise BatchError("injected failure")
                except BatchError as e:
                    return self.fail(400, str(e))
                state["records"] = records
                save(state)
                return self.ok({"deletes": body.get("deletes") or [], "posts": created})

            if len(parts) == 3 and method == "POST":
                try:
                    record = create(state["records"], body)
                except BatchError as e:
                    return self.fail(400, str(e))
                save(state)
                return self.ok(record)
            if len(parts) == 4 and method == "DELETE":
                before = len(state["records"])
                state["records"] = [r for r in state["records"] if r["id"] != parts[3]]
                if len(state["records"]) == before:
                    return self.fail(404, "record not found")
                save(state)
                return self.ok({"id": parts[3]})
            return self.fail(404, "not implemented by the fake")

    def do_GET(self):
        self.handle_any("GET")

    def do_POST(self):
        self.handle_any("POST")

    def do_DELETE(self):
        self.handle_any("DELETE")


server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
with open(STATE + ".port", "w") as f:
    f.write(str(server.server_address[1]))
server.serve_forever()
