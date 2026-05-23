"""
Listmonk harness-adapter entry point.
Starts listmonk on port 9001, then exposes a proxy on port 9000 that:
  - responds 200 to GET /healthz  (satisfies operator readiness probe)
  - forwards all other traffic to listmonk
"""
import os
import subprocess
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

PROXY_PORT = 9000
LISTMONK_PORT = 9001
CONFIG_PATH = "/tmp/listmonk-config.toml"


def _write_config() -> None:
    db_url = os.environ.get("DATABASE_URL", "")
    if db_url:
        u = urlparse(db_url)
        host = u.hostname or "postgres"
        port = u.port or 5432
        user = u.username or "postgres"
        pwd = u.password or "postgres"
        db = (u.path or "/listmonk").lstrip("/")
    else:
        host, port, user, pwd, db = "postgres", 5432, "postgres", "postgres", "listmonk"

    config = f"""[app]
address = "0.0.0.0:{LISTMONK_PORT}"
admin_username = "admin"
admin_password = "harness123"

[db]
host = "{host}"
port = {port}
user = "{user}"
password = "{pwd}"
database = "{db}"
ssl_mode = "disable"
max_open = 10
max_idle = 5
max_lifetime = "300s"
"""
    with open(CONFIG_PATH, "w") as f:
        f.write(config)


class _ProxyHandler(BaseHTTPRequestHandler):
    def _do(self) -> None:
        if self.path == "/healthz":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"ok")
            return

        # F5 — frontend bug injector. When FP_F5_BROKEN_FRONTEND=1 is set,
        # the wrapper intercepts HTML-bound requests (`/` or any path the
        # client expects HTML for) and returns a 500 with a broken page,
        # mimicking a frontend served as garbage. The operator's failure
        # path detects the test-suite failure on the frontend root and
        # produces a real FailureReport whose ground truth is F5
        # (frontend / application).
        if os.environ.get("FP_F5_BROKEN_FRONTEND") == "1":
            accept = self.headers.get("Accept", "") or ""
            if self.path in ("/", "") or self.path.startswith("/admin") or self.path.startswith("/static") or "text/html" in accept:
                self.send_response(500)
                self.send_header("Content-Type", "text/html")
                self.end_headers()
                self.wfile.write(b"<html><body><h1>FP_F5_BROKEN_FRONTEND</h1>"
                                 b"<script>throw new Error('frontend bug "
                                 b"injected by harness-adapter F5');</script>"
                                 b"</body></html>")
                return

        # F10 — flaky-test injector at the PROXY level. When
        # FP_F10_FLAKY=1, with probability 0.5 per request the proxy
        # returns 500 on any /api/* path. The smoke suite hits 5 API
        # endpoints; at least one will fail with ~97 % probability,
        # so the operator captures a FailureReport whose root cause
        # is "the application is flaky / there is no real fault" —
        # the RQ4 hallucination control.
        if (os.environ.get("FP_F10_FLAKY") == "1"
                and self.path.startswith("/api")
                and __import__("random").random() < 0.5):
            self.send_response(500)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"error":"FP_F10_FLAKY: deliberate flake"}')
            return
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length) if length > 0 else None
        target = f"http://127.0.0.1:{LISTMONK_PORT}{self.path}"
        hdrs = {k: v for k, v in self.headers.items()
                if k.lower() not in ("host", "transfer-encoding", "connection")}
        req = urllib.request.Request(target, data=body, method=self.command, headers=hdrs)
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                self.send_response(resp.status)
                for k, v in resp.headers.items():
                    if k.lower() not in ("transfer-encoding", "connection"):
                        self.send_header(k, v)
                self.end_headers()
                self.wfile.write(resp.read())
        except urllib.error.HTTPError as exc:
            self.send_response(exc.code)
            for k, v in exc.headers.items():
                if k.lower() not in ("transfer-encoding", "connection"):
                    self.send_header(k, v)
            self.end_headers()
            self.wfile.write(exc.read())
        except Exception as exc:
            self.send_response(502)
            self.end_headers()
            self.wfile.write(f"proxy error: {exc}".encode())

    do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = _do

    def log_message(self, *args) -> None:
        pass


if __name__ == "__main__":
    _write_config()
    proc = subprocess.Popen(["/listmonk/listmonk", f"--config={CONFIG_PATH}"])

    # Give listmonk a moment to bind its port
    time.sleep(3)

    server = ThreadingHTTPServer(("0.0.0.0", PROXY_PORT), _ProxyHandler)
    t = threading.Thread(target=server.serve_forever, daemon=True)
    t.start()
    print(f"[wrapper] proxy :{PROXY_PORT} → listmonk :{LISTMONK_PORT}", flush=True)

    exit(proc.wait())
