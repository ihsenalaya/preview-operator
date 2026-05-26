#!/usr/bin/env python3
"""k8sgpt-replay.py — Fair post-teardown K8sGPT comparator.

Reads each FailureReport YAML, synthesizes the minimal K8s resources
(Pods, Jobs, Services, Deployments, Events, ConfigMaps) reflecting the
failure state encoded in evidenceItems, applies them to a fresh
namespace on the live AKS cluster, runs `k8sgpt analyze --namespace
<ns>`, captures the JSON result, then deletes the namespace.

The output is comparable to live K8sGPT: same binary, same analyzers,
same backend, but operating on operator-captured evidence — the regime
the operator's diagnoser is designed for.

Output:
  results-baselines-post-teardown/<subject>/<F>/<r>/k8sgpt-pt.json
"""
from __future__ import annotations
import argparse, glob, json, os, re, subprocess, sys, time, uuid
from pathlib import Path
import yaml

REPO = Path('/home/azureuser/preview-operator')
RESULTS_MATRIX = REPO / 'experiments/failure-provenance/results-matrix'
RESULTS_MULTIAPP = REPO / 'experiments/failure-provenance/results-multiapp'
OUT_BASE = REPO / 'experiments/failure-provenance/results-baselines-post-teardown'
K8SGPT = REPO / 'experiments/failure-provenance/bin/k8sgpt-0.4.21'
KUBECTL = 'kubectl'

NS_PREFIX = 'fp-replay-'

def kubectl(*args, **kw):
    return subprocess.run([KUBECTL, *args], capture_output=True, text=True, **kw)

def parse_resource(s: str) -> tuple[str, str]:
    """Parse 'Job/postgres-migrate' or 'pod/foo-abc container/bar' → (kind, name)."""
    s = s.strip()
    # take the first 'Kind/name' token
    m = re.match(r'([A-Za-z]+)\s*/\s*([A-Za-z0-9][A-Za-z0-9._-]*)', s)
    if not m:
        return ('', '')
    kind = m.group(1).capitalize()
    name = m.group(2).lower()
    # truncate name to k8s 63-char limit
    return (kind, name[:63])

def infer_pod_status(messages: list[str]) -> dict:
    """Inspect evidence messages and pick a containerStatus matching what
    K8sGPT analyzers detect on a live failed cluster."""
    blob = ' '.join(messages).lower()
    if 'crashloopbackoff' in blob or 'back-off restarting' in blob:
        return {'phase': 'Running',
                'containerStatuses': [{
                    'name': 'app', 'image': 'busybox:latest',
                    'ready': False, 'restartCount': 5,
                    'state': {'waiting': {
                        'reason': 'CrashLoopBackOff',
                        'message': 'Back-off restarting failed container'}}}]}
    if 'imagepullbackoff' in blob or 'errimagepull' in blob or 'manifest unknown' in blob:
        return {'phase': 'Pending',
                'containerStatuses': [{
                    'name': 'app', 'image': 'invalid/img:nope',
                    'ready': False, 'restartCount': 0,
                    'state': {'waiting': {
                        'reason': 'ImagePullBackOff',
                        'message': 'rpc error: code = Unknown desc = manifest unknown'}}}]}
    if 'configmap' in blob and ('not found' in blob or 'missing' in blob):
        return {'phase': 'Pending',
                'containerStatuses': [{
                    'name': 'app', 'image': 'busybox:latest',
                    'ready': False, 'restartCount': 0,
                    'state': {'waiting': {
                        'reason': 'CreateContainerConfigError',
                        'message': 'ConfigMap referenced not found'}}}]}
    if 'oomkilled' in blob:
        return {'phase': 'Failed',
                'containerStatuses': [{
                    'name': 'app', 'image': 'busybox:latest',
                    'ready': False, 'restartCount': 3,
                    'state': {'waiting': {'reason': 'CrashLoopBackOff', 'message': 'OOM'}},
                    'lastState': {'terminated': {'reason': 'OOMKilled', 'exitCode': 137}}}]}
    # default: pod looks running but with conditions failing
    return {'phase': 'Running',
            'containerStatuses': [{
                'name': 'app', 'image': 'busybox:latest',
                'ready': True, 'restartCount': 0,
                'state': {'running': {'startedAt': '2026-01-01T00:00:00Z'}}}]}

def synth_resources(failurereport: dict, namespace: str) -> list[dict]:
    """Generate K8s manifests from a FailureReport's evidenceItems."""
    items = failurereport.get('status', {}).get('evidenceItems', []) or []
    # Group by (kind, name): collect messages
    groups: dict[tuple[str, str], list[str]] = {}
    for it in items:
        kind, name = parse_resource(str(it.get('resource', '')))
        if not kind or not name:
            continue
        groups.setdefault((kind, name), []).append(str(it.get('message', '')))

    out = []
    for (kind, name), msgs in groups.items():
        if kind in ('Pod',):
            st = infer_pod_status(msgs)
            spec = {
                'restartPolicy': 'Always',
                'containers': [{
                    'name': 'app',
                    'image': 'k8s.gcr.io/pause:3.9',
                    'imagePullPolicy': 'IfNotPresent',
                }],
            }
            out.append({
                'apiVersion': 'v1', 'kind': 'Pod',
                'metadata': {'name': name, 'namespace': namespace,
                             'labels': {'app': name}},
                'spec': spec,
                '__status__': st,
            })
        elif kind in ('Job',):
            out.append({
                'apiVersion': 'batch/v1', 'kind': 'Job',
                'metadata': {'name': name, 'namespace': namespace},
                'spec': {
                    'backoffLimit': 1,
                    'completions': 1,
                    'template': {
                        'spec': {
                            'restartPolicy': 'Never',
                            'containers': [{
                                'name': 'app',
                                'image': 'k8s.gcr.io/pause:3.9',
                                'command': ['false'],
                            }],
                        },
                    },
                },
                '__status__': {
                    'failed': 2,
                    'conditions': [{
                        'type': 'Failed', 'status': 'True',
                        'reason': 'BackoffLimitExceeded',
                        'message': 'Job has reached the specified backoff limit'}],
                },
            })
        elif kind in ('Service',):
            out.append({
                'apiVersion': 'v1', 'kind': 'Service',
                'metadata': {'name': name, 'namespace': namespace},
                'spec': {
                    'selector': {'app': f'nonexistent-{uuid.uuid4().hex[:6]}'},
                    'ports': [{'port': 80, 'targetPort': 8080}],
                },
            })
        elif kind in ('Deployment',):
            out.append({
                'apiVersion': 'apps/v1', 'kind': 'Deployment',
                'metadata': {'name': name, 'namespace': namespace,
                             'labels': {'app': name}},
                'spec': {
                    'replicas': 1,
                    'selector': {'matchLabels': {'app': name}},
                    'template': {
                        'metadata': {'labels': {'app': name}},
                        'spec': {
                            'containers': [{
                                'name': 'app',
                                'image': 'k8s.gcr.io/pause:3.9',
                            }],
                        },
                    },
                },
            })
        # ConfigMap / Endpoints / etc. — skip; not detected by k8sgpt analyzers
    # Always-on: build at least one Pod from KubernetesEvent that
    # references no Pod/Job (fallback synthetic pod with CLBO).
    if not out:
        out.append({
            'apiVersion': 'v1', 'kind': 'Pod',
            'metadata': {'name': 'evidence-pod', 'namespace': namespace,
                         'labels': {'app': 'evidence-pod'}},
            'spec': {
                'restartPolicy': 'Always',
                'containers': [{
                    'name': 'app',
                    'image': 'k8s.gcr.io/pause:3.9',
                }],
            },
            '__status__': {
                'phase': 'Running',
                'containerStatuses': [{
                    'name': 'app', 'image': 'k8s.gcr.io/pause:3.9',
                    'ready': False, 'restartCount': 5,
                    'state': {'waiting': {
                        'reason': 'CrashLoopBackOff',
                        'message': 'Back-off restarting failed container'}}}],
            },
        })
    return out

def apply_resources(resources: list[dict], namespace: str) -> str:
    """Apply manifests + patch status subresource. Returns error or ''."""
    # Apply spec part first
    spec_manifests = []
    status_patches = []
    for r in resources:
        status = r.pop('__status__', None)
        spec_manifests.append(r)
        if status:
            status_patches.append((r['kind'], r['metadata']['name'], status))
    bundle = '\n---\n'.join(yaml.dump(m, default_flow_style=False) for m in spec_manifests)
    p = subprocess.run([KUBECTL, 'apply', '-n', namespace, '-f', '-'],
                       input=bundle, capture_output=True, text=True, timeout=60)
    if p.returncode != 0:
        return f'apply failed: {p.stderr[:300]}'
    # Patch status subresource for each
    for kind, name, status in status_patches:
        patch = json.dumps({'status': status})
        # Pod and Job both support status subresource patch
        p = subprocess.run([KUBECTL, 'patch', kind.lower(), name, '-n', namespace,
                            '--subresource=status', '--type=merge',
                            '-p', patch],
                           capture_output=True, text=True, timeout=30)
        # status patch may fail for some kinds (e.g. Service); ignore
    return ''

def run_k8sgpt(namespace: str) -> dict:
    cmd = [str(K8SGPT), 'analyze',
           '--namespace', namespace,
           '--filter', 'Pod,Job,Service,Deployment,ReplicaSet,StatefulSet,CronJob,PersistentVolumeClaim,ConfigMap',
           '--backend', 'azureopenai',
           '--explain', '--no-cache',
           '--output', 'json']
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=240)
    raw = p.stdout
    if not raw:
        return {'_error': p.stderr.strip()[:500] or 'empty stdout',
                '_returncode': p.returncode}
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        m = re.search(r'\{[\s\S]*\}', raw)
        if m:
            try:
                return json.loads(m.group(0))
            except json.JSONDecodeError:
                pass
        return {'_error': 'unparseable stdout',
                '_raw': raw[:500],
                '_returncode': p.returncode}

def process_one(fr_path: Path, subject: str, scenario: str, rep: str, force: bool = False) -> tuple[str, str]:
    out_path = OUT_BASE / subject / scenario / rep / 'k8sgpt-pt.json'
    if not force and out_path.exists() and out_path.stat().st_size > 50:
        try:
            data = json.loads(out_path.read_text())
            if '_error' not in data and 'error' not in data:
                return ('skip', str(out_path))
        except Exception:
            pass
    out_path.parent.mkdir(parents=True, exist_ok=True)

    with fr_path.open() as f:
        fr = yaml.safe_load(f)

    ns = f'{NS_PREFIX}{subject[:3]}-{scenario.lower()}-{rep}'[:63]
    # Create namespace fresh
    subprocess.run([KUBECTL, 'delete', 'namespace', ns,
                    '--ignore-not-found', '--wait=true', '--timeout=60s'],
                   capture_output=True, text=True, timeout=120)
    p = subprocess.run([KUBECTL, 'create', 'namespace', ns],
                       capture_output=True, text=True, timeout=30)
    if p.returncode != 0:
        return ('fail', f'ns create failed: {p.stderr[:200]}')

    try:
        resources = synth_resources(fr, ns)
        err = apply_resources(resources, ns)
        if err:
            out_path.write_text(json.dumps({'_error': err}))
            return ('fail', err)
        # let kube-apiserver settle for a moment
        time.sleep(3)
        result = run_k8sgpt(ns)
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
    ap.add_argument('--limit', type=int, default=0,
                    help='dev: only process first N captures')
    ap.add_argument('--force', action='store_true',
                    help='re-process all captures even if valid output exists')
    args = ap.parse_args()
    jobs = discover(args.subset, args.max_reps)
    if args.limit:
        jobs = jobs[:args.limit]
    print(f'[k8sgpt-replay] discovered {len(jobs)} captures (force={args.force})', flush=True)
    ok = skip = fail = 0
    t0 = time.time()
    for i, (subject, scenario, rep, fr) in enumerate(jobs, 1):
        status, msg = process_one(fr, subject, scenario, rep, force=args.force)
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
    print(f'[k8sgpt-replay] DONE: ok={ok} skip={skip} fail={fail} '
          f'elapsed={time.time()-t0:.0f}s', flush=True)

if __name__ == '__main__':
    main()
