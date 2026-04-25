import os
import psycopg2
from flask import Flask, request, redirect

app = Flask(__name__)

DATABASE_URL = os.environ.get("DATABASE_URL", "")
POSTGRES_DB = os.environ.get("POSTGRES_DB", "")
POSTGRES_USER = os.environ.get("POSTGRES_USER", "")


def log(message, **fields):
    details = " ".join(f"{key}={value}" for key, value in fields.items() if value)
    print(f"[db] {message}" + (f" {details}" if details else ""), flush=True)


def get_conn():
    log("Opening PostgreSQL connection", database=POSTGRES_DB, user=POSTGRES_USER)
    return psycopg2.connect(DATABASE_URL)


def init_db():
    conn = get_conn()
    cur = conn.cursor()
    cur.execute("""
        CREATE TABLE IF NOT EXISTS messages (
            id        SERIAL PRIMARY KEY,
            author    TEXT NOT NULL DEFAULT 'anonymous',
            text      TEXT NOT NULL,
            created_at TIMESTAMPTZ DEFAULT NOW()
        )
    """)
    conn.commit()
    cur.close()
    conn.close()
    log("Initialized PostgreSQL schema", table="messages")


def render(pg_version, rows, db_ok, error=""):
    status_color = "#22c55e" if db_ok else "#ef4444"
    status_label = "Connecté" if db_ok else "Erreur"

    rows_html = ""
    for r in rows:
        rows_html += f"""
        <tr>
            <td>{r[0]}</td>
            <td><strong>{r[1]}</strong></td>
            <td>{r[2]}</td>
            <td>{r[3].strftime('%d/%m/%Y %H:%M:%S')}</td>
        </tr>"""

    if not rows_html:
        rows_html = '<tr><td colspan="4" class="empty">Aucun message — soyez le premier !</td></tr>'

    env_items = ""
    for key in ("POSTGRES_USER", "POSTGRES_DB", "PREVIEW_BRANCH", "PREVIEW_PR", "ENVIRONMENT"):
        val = os.environ.get(key, "—")
        env_items += f'<div class="env-item"><span class="key">{key}</span>{val}</div>'

    return f"""<!DOCTYPE html>
<html lang="fr">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Cellenza Demo App</title>
  <style>
    *, *::before, *::after {{ box-sizing: border-box; margin: 0; padding: 0; }}
    body   {{ font-family: system-ui, -apple-system, sans-serif; background: #f1f5f9; color: #1e293b; }}
    header {{ background: linear-gradient(135deg, #1e293b, #334155);
              color: white; padding: 1.5rem 2rem; display: flex; align-items: center; gap: 1rem; }}
    header h1  {{ font-size: 1.4rem; }}
    header p   {{ font-size: 0.85rem; opacity: .65; margin-top: .2rem; }}
    .badge     {{ display: inline-block; background: {status_color}; color: white;
                  padding: .2rem .65rem; border-radius: 999px; font-size: .75rem;
                  font-weight: 700; letter-spacing: .5px; }}
    .container {{ max-width: 860px; margin: 2rem auto; padding: 0 1rem; display: grid; gap: 1.25rem; }}
    .card      {{ background: white; border-radius: 10px; padding: 1.5rem;
                  box-shadow: 0 1px 4px rgba(0,0,0,.08); }}
    .card h2   {{ font-size: .9rem; text-transform: uppercase; letter-spacing: .8px;
                  color: #94a3b8; margin-bottom: 1rem; }}
    .db-info   {{ display: flex; align-items: center; gap: .75rem; flex-wrap: wrap; }}
    .version   {{ font-family: monospace; font-size: .82rem; color: #64748b; }}
    .error     {{ color: #ef4444; font-size: .82rem; margin-top: .5rem; font-family: monospace; }}
    .env-grid  {{ display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
                  gap: .5rem; margin-top: 1rem; }}
    .env-item  {{ background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 6px;
                  padding: .5rem .75rem; font-size: .8rem; }}
    .env-item .key {{ display: block; font-family: monospace; color: #7c3aed;
                      font-weight: 700; margin-bottom: .15rem; }}
    form       {{ display: flex; gap: .6rem; flex-wrap: wrap; }}
    input[type=text] {{ flex: 1; min-width: 120px; padding: .55rem .8rem;
                        border: 1px solid #e2e8f0; border-radius: 6px; font-size: .9rem;
                        outline: none; transition: border-color .2s; }}
    input[type=text]:focus {{ border-color: #7c3aed; }}
    button     {{ background: #7c3aed; color: white; border: none; cursor: pointer;
                  padding: .55rem 1.3rem; border-radius: 6px; font-weight: 700;
                  font-size: .9rem; transition: background .2s; }}
    button:hover {{ background: #6d28d9; }}
    table      {{ width: 100%; border-collapse: collapse; margin-top: 1.25rem; }}
    thead th   {{ text-align: left; padding: .5rem .6rem; font-size: .75rem; font-weight: 700;
                  text-transform: uppercase; letter-spacing: .5px; color: #94a3b8;
                  border-bottom: 2px solid #f1f5f9; }}
    tbody td   {{ padding: .65rem .6rem; border-bottom: 1px solid #f8fafc; font-size: .88rem; }}
    tbody tr:hover td {{ background: #fafafa; }}
    .empty     {{ text-align: center; color: #cbd5e1; padding: 2rem; font-style: italic; }}
    footer     {{ text-align: center; color: #94a3b8; font-size: .78rem; padding: 2rem; }}
  </style>
</head>
<body>
  <header>
    <div>
      <h1>&#128640; Cellenza Demo App</h1>
      <p>Preview environment — base de données PostgreSQL injectée par l'opérateur</p>
    </div>
  </header>

  <div class="container">

    <div class="card">
      <h2>Statut PostgreSQL</h2>
      <div class="db-info">
        <span class="badge">{status_label}</span>
        <span class="version">{pg_version}</span>
      </div>
      {f'<p class="error">{error}</p>' if error else ''}
      <div class="env-grid">{env_items}</div>
    </div>

    <div class="card">
      <h2>Test Cellenza Operator</h2>
      <form method="POST" action="/add">
        <input type="text" name="author" placeholder="Votre nom" maxlength="50" required>
        <input type="text" name="text"   placeholder="Votre message..." maxlength="200" required>
        <button type="submit">Envoyer</button>
      </form>
      <table>
        <thead>
          <tr><th>#</th><th>Auteur</th><th>Message</th><th>Date</th></tr>
        </thead>
        <tbody>{rows_html}</tbody>
      </table>
    </div>

  </div>
  <footer>Cellenza Operator — demo-app v0.5.1</footer>
</body>
</html>"""


@app.route("/", methods=["GET"])
def index():
    try:
        conn = get_conn()
        cur = conn.cursor()
        cur.execute("SELECT version()")
        pg_version = cur.fetchone()[0].split(",")[0]
        cur.execute("SELECT id, author, text, created_at FROM messages ORDER BY created_at DESC LIMIT 20")
        rows = cur.fetchall()
        cur.close()
        conn.close()
        log("Read messages from PostgreSQL", rows=len(rows))
        return render(pg_version, rows, db_ok=True)
    except Exception as e:
        log("PostgreSQL query failed", error=str(e))
        return render("n/a", [], db_ok=False, error=str(e)), 500


@app.route("/add", methods=["POST"])
def add():
    author = request.form.get("author", "anonymous")[:50]
    text   = request.form.get("text", "")[:200]
    if text.strip():
        conn = get_conn()
        cur  = conn.cursor()
        cur.execute("INSERT INTO messages (author, text) VALUES (%s, %s)", (author, text))
        conn.commit()
        cur.close()
        conn.close()
        log("Inserted message into PostgreSQL", author=author)
    return redirect("/")


@app.route("/healthz")
def healthz():
    return "ok", 200


if __name__ == "__main__":
    init_db()
    app.run(host="0.0.0.0", port=80)
