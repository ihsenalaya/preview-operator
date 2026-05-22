"""
Healthchecks harness-adapter entry point.
Starts uwsgi (healthchecks) on port 8001, proxies on port 8000.
Serves GET /healthz → 200 without touching Django.
"""
import os
import subprocess
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PROXY_PORT = 8000
APP_PORT = 8001


class _ProxyHandler(BaseHTTPRequestHandler):
    def _do(self) -> None:
        if self.path == "/healthz":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"ok")
            return
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length) if length > 0 else None
        target = f"http://127.0.0.1:{APP_PORT}{self.path}"
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
    env = os.environ.copy()

    db_url = os.environ.get("DATABASE_URL", "")
    if db_url:
        u = urllib.parse.urlparse(db_url)
        env["DB"] = "postgres"
        env["DB_HOST"] = u.hostname or "postgres"
        env["DB_PORT"] = str(u.port or 5432)
        env["DB_NAME"] = (u.path or "/hc").lstrip("/")
        env["DB_USER"] = u.username or "postgres"
        env["DB_PASSWORD"] = u.password or ""
        env["DB_SSLMODE"] = "disable"

    env.setdefault("SECRET_KEY", "harness-experiment-key-insecure")
    env.setdefault("ALLOWED_HOSTS", "*")
    env.setdefault("DEBUG", "False")

    proc = subprocess.Popen(
        [
            "uwsgi",
            "--http-socket", f"0.0.0.0:{APP_PORT}",
            "--chdir", "/opt/healthchecks",
            "--module", "hc.wsgi:application",
            "--master",
            "--processes", "2",
            "--harakiri", "30",
            "--buffer-size", "32768",
        ],
        cwd="/opt/healthchecks",
        env=env,
    )

    time.sleep(5)

    server = ThreadingHTTPServer(("0.0.0.0", PROXY_PORT), _ProxyHandler)
    t = threading.Thread(target=server.serve_forever, daemon=True)
    t.start()
    print(f"[wrapper] proxy :{PROXY_PORT} → healthchecks :{APP_PORT}", flush=True)

    exit(proc.wait())
