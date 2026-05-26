"""
Shared isolation probe service.
Manages the run_log table independently of the subject application.
DATABASE_URL is injected by the operator from postgres-credentials.
"""
import os
import psycopg2
import psycopg2.extras
from flask import Flask, jsonify, request

app = Flask(__name__)
DATABASE_URL = os.environ.get("DATABASE_URL", "")


def get_conn():
    return psycopg2.connect(DATABASE_URL, cursor_factory=psycopg2.extras.RealDictCursor)


def init_db():
    with get_conn() as c, c.cursor() as cur:
        cur.execute("""
            CREATE TABLE IF NOT EXISTS run_log (
                id         SERIAL PRIMARY KEY,
                suite      TEXT NOT NULL,
                created_at TIMESTAMPTZ DEFAULT NOW()
            )
        """)


try:
    init_db()
except Exception as exc:
    print(f"[probe] init_db deferred: {exc}", flush=True)


@app.get("/healthz")
def healthz():
    return "ok"


@app.get("/api/run-log")
def get_run_log():
    try:
        with get_conn() as c, c.cursor() as cur:
            cur.execute(
                "SELECT suite, COUNT(*) AS cnt FROM run_log GROUP BY suite ORDER BY suite"
            )
            return jsonify({row["suite"]: row["cnt"] for row in cur.fetchall()})
    except Exception as exc:
        return jsonify({"error": str(exc)}), 500


@app.post("/api/run-log")
def post_run_log():
    body = request.get_json(force=True)
    try:
        with get_conn() as c, c.cursor() as cur:
            cur.execute("INSERT INTO run_log (suite) VALUES (%s)", (body.get("suite", "unknown"),))
        return jsonify({"ok": True}), 201
    except Exception as exc:
        return jsonify({"error": str(exc)}), 500


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=9090)
