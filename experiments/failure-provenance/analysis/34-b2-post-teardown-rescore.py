#!/usr/bin/env python3
"""34-b2-post-teardown-rescore.py — Score the post-teardown K8sGPT and
Kagent replay outputs (`results-baselines-post-teardown/<subject>/<F>/<r>/
{k8sgpt-pt,kagent-pt}.json`) with the same vocabulary-aligned matcher
used in 14c-b2-multiapp-rescore.py.

Outputs:
  experiments/failure-provenance/results-matrix/results-b2-post-teardown.csv
"""
from __future__ import annotations
import csv, json, pathlib, re, sys

ROOT = pathlib.Path('/home/azureuser/preview-operator/experiments/failure-provenance')
RESULTS = ROOT / 'results-baselines-post-teardown'
OUT_CSV = ROOT / 'results-matrix' / 'results-b2-post-teardown.csv'

SUBJECT_IDX = {
    's2-listmonk': 0, 's3-healthchecks': 1, 's4-umami': 2, 's5-petclinic': 3,
}
SCENARIO_ROLE = {
    'F1': 'migration-job', 'F2': 'app', 'F3': 'app',
    'F4': 'backend', 'F5': 'frontend', 'F6': 'app',
    'F7': 'service', 'F8': 'backend', 'F9': 'seed-job',
    'F10': 'test-suite',
}
SCENARIO_CATEGORY = {
    'F1': 'database', 'F2': 'configuration', 'F3': 'infrastructure',
    'F4': 'application', 'F5': 'application', 'F6': 'infrastructure',
    'F7': 'infrastructure', 'F8': 'observability', 'F9': 'database',
    'F10': 'test-reliability',
}

KEYWORD_ROLES = [
    (r'\bpostgres-?migrate\b|\bmigration.?job\b|\balembic\b|\bprisma.?migrate\b|\bflyway\b|\bdjango.?migrate\b', 'migration-job'),
    (r'\bseed-?job\b|\bseed\b|\bai-?seed\b', 'seed-job'),
    (r'\btest-?suite\b|\bpytest\b|\bplaywright\b|\bflaky\b|\btests?\b', 'test-suite'),
    (r'\bsvc-?backend\b|\bservice\b|\bendpoints?\b|\bselector\b', 'service'),
    (r'\bfrontend\b|\bcatalogue\b|\bui\b', 'frontend'),
    (r'\bbackend\b|\bapi\b|\broute\b|\blistmonk\b|\bumami\b|\bhealthchecks\b|\bhc-?web\b|\bpetclinic\b|\bspring-?petclinic\b', 'backend'),
    (r'\bapp-?deployment\b|\bapp\b|\bapplication\b|\bdeployment\b', 'app'),
]

SUBJECT_ALIASES_ROLE = {
    'migration-job': 'migration-job',
    'postgres-migrate': 'migration-job',
    'app': 'app', 'application': 'app', 'app-deployment': 'app',
    'deployment': 'app', 'listmonk': 'app', 'healthchecks': 'app',
    'hc-web': 'app', 'umami': 'app', 'petclinic': 'app', 'spring-petclinic': 'app',
    'backend': 'backend', 'svc-backend': 'backend', 'api': 'backend',
    'frontend': 'frontend', 'ui': 'frontend',
    'service': 'service', 'svc': 'service',
    'seed-job': 'seed-job', 'seed': 'seed-job',
    'test-suite': 'test-suite', 'tests': 'test-suite', 'flaky': 'test-suite',
}

def normalise(s): return re.sub(r'\s+', ' ', re.sub(r'[^a-z0-9\- ]', '', (s or '').lower().strip()))

def fingerprint_role(text):
    if not text: return ''
    t = text.lower()
    for pat, role in KEYWORD_ROLES:
        if re.search(pat, t):
            return role
    return ''

def k8sgpt_extract(blob, scenario):
    if not blob or '_error' in blob or 'error' in blob and not blob.get('results'):
        return ('', '')
    results = blob.get('results') or blob.get('problems') or []
    if not results:
        return ('', '')
    # Scan ALL results' text fields (the synthesized Pod is often not [0]
    # because ConfigMap istio-ca-root-cert or other AKS noise gets sorted first).
    best_role = ''
    for p in results:
        parts = [str(p.get('kind', '')), str(p.get('name', '')),
                 str(p.get('details', ''))]
        for err in p.get('error', []) or []:
            if isinstance(err, dict):
                parts.append(str(err.get('Text', '')))
        text = ' '.join(parts)
        role = fingerprint_role(text)
        if role:
            best_role = role
            break
    return (best_role, SCENARIO_CATEGORY.get(scenario, '') if best_role else '')

def kagent_extract(blob, scenario):
    if not blob or '_error' in blob:
        return ('', '')
    # Replay output stores diagnosis directly in _diagnosis
    d = blob.get('_diagnosis')
    if isinstance(d, dict):
        comp = (d.get('component') or '').strip()
        cat = (d.get('category') or '').strip()
        role = fingerprint_role(comp) or comp
        return (role, cat)
    # Fallback: drill into JSON-RPC envelope like 14c
    result = blob.get('_full', {}).get('result') or blob.get('result') or {}
    artifacts = result.get('artifacts') or []
    agent_text = ''
    for a in artifacts:
        for part in a.get('parts') or []:
            if isinstance(part, dict) and part.get('kind') == 'text':
                agent_text += (part.get('text') or '') + '\n'
    if not agent_text:
        return ('', '')
    t = re.sub(r'\s*```\s*', '', re.sub(r'```(?:json)?\s*', '', agent_text.strip()))
    m = re.search(r'\{[^{}]*"component"[^{}]*\}', t)
    if m:
        try:
            d = json.loads(m.group(0))
            comp = (d.get('component') or '').strip()
            cat = (d.get('category') or '').strip()
            role = fingerprint_role(comp) or comp
            return (role, cat)
        except Exception:
            pass
    return (fingerprint_role(agent_text), '')

def aligned_for(subject, scenario, role):
    if not role: return False
    gt = SCENARIO_ROLE.get(scenario)
    if not gt: return False
    return SUBJECT_ALIASES_ROLE.get(role, role) == gt

def main():
    rows = []
    for subj_dir in sorted(RESULTS.glob('s*-*')):
        subject = subj_dir.name
        if subject not in SUBJECT_IDX:
            continue
        for fdir in sorted(subj_dir.glob('F*')):
            scenario = fdir.name
            if scenario not in SCENARIO_ROLE:
                continue
            for rdir in sorted(fdir.glob('r*')):
                rep = rdir.name
                k8s_path = rdir / 'k8sgpt-pt.json'
                kag_path = rdir / 'kagent-pt.json'
                if not k8s_path.exists() and not kag_path.exists():
                    continue
                row = {
                    'subject': subject, 'scenario': scenario, 'rep': rep,
                    'gt_role': SCENARIO_ROLE[scenario],
                    'gt_category': SCENARIO_CATEGORY.get(scenario, ''),
                    'b2a_k8sgpt_pt_role': '', 'b2a_k8sgpt_pt_aligned': '0',
                    'b2b_kagent_pt_role': '', 'b2b_kagent_pt_category': '',
                    'b2b_kagent_pt_aligned': '0',
                }
                if k8s_path.exists():
                    try:
                        b = json.loads(k8s_path.read_text())
                        role, cat = k8sgpt_extract(b, scenario)
                        row['b2a_k8sgpt_pt_role'] = role
                        row['b2a_k8sgpt_pt_aligned'] = '1' if aligned_for(subject, scenario, role) else '0'
                    except Exception as e:
                        row['b2a_k8sgpt_pt_role'] = f'parse-error: {e}'
                if kag_path.exists():
                    try:
                        b = json.loads(kag_path.read_text())
                        role, cat = kagent_extract(b, scenario)
                        row['b2b_kagent_pt_role'] = role
                        row['b2b_kagent_pt_category'] = cat
                        row['b2b_kagent_pt_aligned'] = '1' if aligned_for(subject, scenario, role) else '0'
                    except Exception as e:
                        row['b2b_kagent_pt_role'] = f'parse-error: {e}'
                rows.append(row)
    if not rows:
        print('no rows', file=sys.stderr)
        return 1
    OUT_CSV.parent.mkdir(parents=True, exist_ok=True)
    with OUT_CSV.open('w', newline='') as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        for r in rows:
            w.writerow(r)
    n_a = sum(1 for r in rows if r['b2a_k8sgpt_pt_aligned'] == '1')
    n_b = sum(1 for r in rows if r['b2b_kagent_pt_aligned'] == '1')
    n = len(rows)
    print(f'wrote {OUT_CSV}')
    print(f'  rows: {n}')
    print(f'  K8sGPT-PT aligned: {n_a}/{n} = {n_a/n*100:.1f}%')
    print(f'  Kagent-PT aligned: {n_b}/{n} = {n_b/n*100:.1f}%')
    # Per-subject breakdown
    print('  Per subject (K8sGPT-PT / Kagent-PT):')
    for subj in SUBJECT_IDX:
        sub_rows = [r for r in rows if r['subject'] == subj]
        if not sub_rows: continue
        a = sum(1 for r in sub_rows if r['b2a_k8sgpt_pt_aligned'] == '1')
        b = sum(1 for r in sub_rows if r['b2b_kagent_pt_aligned'] == '1')
        print(f'    {subj}: K8sGPT {a}/{len(sub_rows)}  Kagent {b}/{len(sub_rows)}')
    return 0

if __name__ == '__main__':
    sys.exit(main())
