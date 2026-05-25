#!/usr/bin/env python3
"""kagent-replay.py — Fair post-teardown Kagent comparator.

For each FailureReport YAML, synthesises K8s resources (same harness as
k8sgpt-replay.py), creates a temp namespace on AKS, and POSTs an A2A
message/send to Kagent's k8s-agent with a fixed SRE diagnostic prompt
naming the namespace. Captures the JSON reply, then tears down.

Uses kubectl port-forward to reach the in-cluster service
kagent-system/k8s-agent:8080.

Output:
  results-baselines-post-teardown/<subject>/<F>/<r>/kagent-pt.json
"""
from __future__ import annotations
import argparse, json, os, re, signal, socket, subprocess, sys, time
from pathlib import Path
import urllib.request, urllib.error
import yaml

# Re-use synthesizer from k8sgpt-replay (in the same directory)
sys.path.insert(0, str(Path(__file__).parent))
from k8sgpt_replay import synth_resources, apply_resources

REPO = Path('/home/azureuser/preview-operator')
RESULTS_MATRIX = REPO / 'experiments/failure-provenance/results-matrix'
RESULTS_MULTIAPP = REPO / 'experiments/failure-provenance/results-multiapp'
OUT_BASE = REPO / 'experiments/failure-provenance/results-baselines-post-teardown'
KUBECTL = 'kubectl'

NS_PREFIX = 'kg-replay-'
KAGENT_LOCAL_PORT = 18080

KAGENT_PROMPT = (
    "You are diagnosing a failing Kubernetes preview environment in the "
    "namespace `{ns}`. Use kubectl tools to inspect the namespace, "
    "find the single most likely root cause, and reply with ONLY a JSON "
    "object of the form:\n\n"
    "{{\n"
    '  "component": "<short identifier of the failing component>",\n'
    '  "category":  "<database | configuration | infrastructure | '
    'application | observability | test-reliability>",\n'
    '  "probableCause": "<one sentence>"\n'
    "}}\n\n"
    "Use the namespace `{ns}` and inspect its events, pods, "
    "deployments, services, jobs, endpoints and the most recent container "
    "logs. Do not return any prose outside the JSON object."
)

def port_forward_open(port: int) -> subprocess.Popen:
    """Open `kubectl port-forward` in background to kagent-system/k8s-agent."""
    cmd = [KUBECTL, '-n', 'kagent-system', 'port-forward',
           'svc/k8s-agent', f'{port}:8080']
    p = subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                         preexec_fn=os.setsid)
    # wait for the port to be open
    deadline = time.time() + 15
    while time.time() < deadline:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.settimeout(0.5)
            try:
                if s.connect_ex(('127.0.0.1', port)) == 0:
                    return p
            except OSError:
                pass
        time.sleep(0.4)
    raise RuntimeError(f'port-forward to k8s-agent did not open on :{port}')

def port_forward_close(p: subprocess.Popen):
    try:
        os.killpg(os.getpgid(p.pid), signal.SIGTERM)
    except Exception:
        pass
    try:
        p.wait(timeout=5)
    except Exception:
        pass

def call_kagent(ns: str, port: int, timeout: int = 240) -> dict:
    url = f'http://127.0.0.1:{port}/'
    msg = KAGENT_PROMPT.format(ns=ns)
    body = json.dumps({
        'jsonrpc': '2.0',
        'id': f'probe-{ns}-{int(time.time())}',
        'method': 'message/send',
        'params': {
            'message': {
                'kind': 'message',
                'role': 'user',
                'messageId': f'probe-{ns}-{int(time.time())}',
                'parts': [{'kind': 'text', 'text': msg}],
            },
        },
    }).encode()
    last_err = ''
    for attempt in range(4):
        req = urllib.request.Request(url, data=body,
                                      headers={'Content-Type': 'application/json'},
                                      method='POST')
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                payload = json.loads(resp.read().decode())
                break
        except (urllib.error.URLError, TimeoutError, ConnectionError,
                __import__('http').client.RemoteDisconnected) as e:
            last_err = str(e)
            time.sleep(3 + attempt * 3)
    else:
        return {'_error': f'retries exhausted: {last_err}'}
    # extract text reply
    raw = ''
    result = payload.get('result', {})
    artifacts = result.get('artifacts', [])
    if artifacts:
        for part in artifacts[0].get('parts', []):
            if part.get('kind') == 'text':
                raw = part.get('text', '')
                break
    diag = None
    m = re.search(r'\{[\s\S]*\}', raw)
    if m:
        try:
            diag = json.loads(m.group(0))
        except json.JSONDecodeError:
            diag = {'_parse_error': True}
    return {'_diagnosis': diag, '_raw_text': raw, '_full': payload}

def process_one(fr_path: Path, subject: str, scenario: str, rep: str, port: int, force: bool = False) -> tuple[str, str]:
    out_path = OUT_BASE / subject / scenario / rep / 'kagent-pt.json'
    if not force and out_path.exists() and out_path.stat().st_size > 80:
        try:
            data = json.loads(out_path.read_text())
            if '_error' not in data and data.get('_diagnosis') is not None:
                return ('skip', str(out_path))
        except Exception:
            pass
    out_path.parent.mkdir(parents=True, exist_ok=True)

    with fr_path.open() as f:
        fr = yaml.safe_load(f)
    ns = f'{NS_PREFIX}{subject[:3]}-{scenario.lower()}-{rep}'[:63]
    subprocess.run([KUBECTL, 'delete', 'namespace', ns,
                    '--ignore-not-found', '--wait=true', '--timeout=60s'],
                   capture_output=True, text=True, timeout=120)
    p = subprocess.run([KUBECTL, 'create', 'namespace', ns],
                       capture_output=True, text=True, timeout=30)
    if p.returncode != 0:
        return ('fail', f'ns create: {p.stderr[:200]}')
    try:
        resources = synth_resources(fr, ns)
        err = apply_resources(resources, ns)
        if err:
            out_path.write_text(json.dumps({'_error': err}))
            return ('fail', err)
        time.sleep(3)
        result = call_kagent(ns, port)
        out_path.write_text(json.dumps(result, indent=2))
        if '_error' in result:
            return ('fail', result.get('_error', '')[:120])
        return ('ok', str(out_path))
    finally:
        subprocess.run([KUBECTL, 'delete', 'namespace', ns,
                        '--ignore-not-found', '--wait=false'],
                       capture_output=True, text=True, timeout=30)

def discover(subset: str, max_reps: int | None) -> list[tuple[str, str, str, Path]]:
    out = []
    parts = set(s.strip() for s in subset.split('+'))
    if 's1' in parts:
        for fr in RESULTS_MATRIX.glob('F*/r*/artifacts/failurereport.yaml'):
            scenario = fr.parents[2].name
            rep = fr.parents[1].name
            out.append(('s1-flask-catalog', scenario, rep, fr))
    for s in ('s2-listmonk', 's3-healthchecks', 's4-umami', 's5-petclinic'):
        if s[:2] in parts:
            for fr in (RESULTS_MULTIAPP / s).glob('F*/r*/artifacts/failurereport.yaml'):
                scenario = fr.parents[2].name
                rep = fr.parents[1].name
                out.append((s, scenario, rep, fr))
    if max_reps:
        seen: dict[tuple[str, str], int] = {}
        kept = []
        for s, sc, r, fr in sorted(out):
            k = (s, sc)
            seen[k] = seen.get(k, 0) + 1
            if seen[k] <= max_reps:
                kept.append((s, sc, r, fr))
        out = kept
    return out

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--subset', default='s2+s3+s4+s5')
    ap.add_argument('--max-reps', type=int, default=3)
    ap.add_argument('--limit', type=int, default=0)
    ap.add_argument('--force', action='store_true',
                    help='re-process all captures even if valid output exists')
    args = ap.parse_args()
    jobs = discover(args.subset, args.max_reps)
    if args.limit:
        jobs = jobs[:args.limit]
    print(f'[kagent-replay] discovered {len(jobs)} captures (force={args.force})', flush=True)
    pf = port_forward_open(KAGENT_LOCAL_PORT)
    try:
        ok = skip = fail = 0
        t0 = time.time()
        for i, (subject, scenario, rep, fr) in enumerate(jobs, 1):
            status, msg = process_one(fr, subject, scenario, rep, KAGENT_LOCAL_PORT, force=args.force)
            if status == 'ok': ok += 1
            elif status == 'skip': skip += 1
            else:
                fail += 1
                print(f'  FAIL {subject}/{scenario}/{rep}: {msg}', flush=True)
            if i % 5 == 0 or i == len(jobs):
                el = time.time() - t0
                rate = i / el if el > 0 else 0
                eta = (len(jobs) - i) / rate if rate > 0 else 0
                print(f'  {i}/{len(jobs)} ok={ok} skip={skip} fail={fail} '
                      f'rate={rate:.2f}/s eta={eta:.0f}s', flush=True)
    finally:
        port_forward_close(pf)
    print(f'[kagent-replay] DONE: ok={ok} skip={skip} fail={fail} '
          f'elapsed={time.time()-t0:.0f}s', flush=True)

if __name__ == '__main__':
    main()
