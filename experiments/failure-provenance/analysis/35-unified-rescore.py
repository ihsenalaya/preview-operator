#!/usr/bin/env python3
"""35-unified-rescore.py — S1-S5 unified vocabulary-aligned rescoring.

Iterates BOTH the S1 capture tree (`results-matrix/`) and the S2-S5
capture tree (`results-multiapp/`) and produces:
  docs/research/failure-provenance/analysis-output/35-unified-rescore/
    per-subject-results.csv       per (subject × scenario × C × engine × rep)
    summary-by-subject.md         aligned top-1 per (subject × engine)
    summary-pooled-s1s5.md        pooled aligned top-1 across S1-S5 by engine
"""
from __future__ import annotations
import argparse, csv, json, math, pathlib, re, sys
from collections import defaultdict

# Reuse the per-subject alias table from 17-multiapp-rescore.
# Filename has hyphen — load via importlib.util.spec_from_file_location.
import importlib.util
_spec = importlib.util.spec_from_file_location(
    "m17",
    '/home/azureuser/preview-operator/experiments/failure-provenance/'
    'analysis/17-multiapp-rescore.py')
m17 = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(m17)
SUBJECT_ALIASES = m17.SUBJECT_ALIASES
GROUND_TRUTH = m17.GROUND_TRUTH
aligned_match = m17.aligned_match
parse_diag_filename = m17.parse_diag_filename
wilson_ci = m17.wilson_ci


def gather_rows(captures_root: pathlib.Path, subject_id_for: callable) -> list[dict]:
    rows: list[dict] = []
    for subject_dir in sorted(captures_root.glob('*')):
        if not subject_dir.is_dir():
            continue
        s_id = subject_id_for(subject_dir.name)
        if s_id is None:
            continue
        for scen_dir in sorted(subject_dir.glob('F*')):
            if not scen_dir.is_dir():
                continue
            scen = scen_dir.name
            gt = GROUND_TRUTH.get(scen)
            if not gt:
                continue
            role = gt['component_role']
            for rep_dir in sorted(scen_dir.glob('r*')):
                if not rep_dir.is_dir():
                    continue
                rep = rep_dir.name
                for diag_file in sorted(rep_dir.glob('diag-*.json')):
                    parsed = parse_diag_filename(diag_file.name)
                    if not parsed:
                        continue
                    conf, engine, mode = parsed
                    try:
                        blob = json.loads(diag_file.read_text())
                    except json.JSONDecodeError:
                        continue
                    comp = (blob.get('diagnosis', {}).get('component', '') or '')
                    cat = (blob.get('diagnosis', {}).get('category', '') or '')
                    aligned = aligned_match(s_id, role, comp)
                    rows.append({
                        'subject_id': s_id,
                        'scenario_id': scen,
                        'rep': rep,
                        'configuration': conf,
                        'engine_mode': f'{engine}-{mode}',
                        'diag_component': comp,
                        'diag_category': cat,
                        'gt_role': role,
                        'gt_category': gt['category'],
                        'top1_aligned': '1' if aligned else '0',
                    })
    return rows


def main() -> int:
    ROOT = pathlib.Path('/home/azureuser/preview-operator/experiments/failure-provenance')
    OUT = pathlib.Path('/home/azureuser/preview-operator/docs/research/failure-provenance/'
                      'analysis-output/35-unified-rescore')
    OUT.mkdir(parents=True, exist_ok=True)

    # S1: results-matrix/F*/r*/diag-*.json — subject is implicit
    def s1_id(_name): return 's1-flask-catalog'
    s1_rows = gather_rows(ROOT / 'results-matrix', s1_id)
    # Wait — results-matrix has F*/r*/ at root, no subject directory.
    # Adjust: simulate a subject-dir layer by passing results-matrix as
    # "the subject" — but then scen_dir.glob('F*') wouldn't work.
    # Re-do S1 traversal explicitly.
    s1_rows = []
    s1_root = ROOT / 'results-matrix'
    for scen_dir in sorted(s1_root.glob('F*')):
        if not scen_dir.is_dir():
            continue
        scen = scen_dir.name
        gt = GROUND_TRUTH.get(scen)
        if not gt:
            continue
        role = gt['component_role']
        for rep_dir in sorted(scen_dir.glob('r*')):
            if not rep_dir.is_dir():
                continue
            rep = rep_dir.name
            for diag_file in sorted(rep_dir.glob('diag-*.json')):
                parsed = parse_diag_filename(diag_file.name)
                if not parsed:
                    continue
                conf, engine, mode = parsed
                try:
                    blob = json.loads(diag_file.read_text())
                except json.JSONDecodeError:
                    continue
                comp = (blob.get('diagnosis', {}).get('component', '') or '')
                cat = (blob.get('diagnosis', {}).get('category', '') or '')
                aligned = aligned_match('s1-flask-catalog', role, comp)
                s1_rows.append({
                    'subject_id': 's1-flask-catalog',
                    'scenario_id': scen, 'rep': rep,
                    'configuration': conf,
                    'engine_mode': f'{engine}-{mode}',
                    'diag_component': comp, 'diag_category': cat,
                    'gt_role': role, 'gt_category': gt['category'],
                    'top1_aligned': '1' if aligned else '0',
                })

    # S2-S5: results-multiapp/<subject>/F*/r*/diag-*.json
    def multi_id(name):
        if name in {'s2-listmonk', 's3-healthchecks', 's4-umami', 's5-petclinic'}:
            return name
        return None
    multi_rows = gather_rows(ROOT / 'results-multiapp', multi_id)

    all_rows = s1_rows + multi_rows
    print(f'  S1 rows: {len(s1_rows)}', flush=True)
    print(f'  S2-S5 rows: {len(multi_rows)}', flush=True)
    print(f'  total rows: {len(all_rows)}', flush=True)

    # Write per-row CSV
    csv_path = OUT / 'per-subject-results.csv'
    with csv_path.open('w', newline='') as f:
        if all_rows:
            w = csv.DictWriter(f, fieldnames=list(all_rows[0].keys()))
            w.writeheader()
            for r in all_rows:
                w.writerow(r)
    print(f'wrote {csv_path} ({len(all_rows)} rows)')

    # Summary by (subject × engine)
    by_se: dict[tuple[str, str], list[int]] = defaultdict(list)
    for r in all_rows:
        by_se[(r['subject_id'], r['engine_mode'])].append(int(r['top1_aligned']))
    md = ['# S1-S5 vocabulary-aligned top-1 by subject × engine', '',
          '| Subject | Engine | n | aligned top-1 | Wilson 95% CI |',
          '|---|---|---|---|---|']
    for (subj, eng), data in sorted(by_se.items()):
        n = len(data); s = sum(data)
        lo, hi = wilson_ci(s, n)
        md.append(f'| {subj} | {eng} | {n} | {100*s/n:5.1f}% ({s}/{n}) | '
                  f'[{100*lo:.1f}%, {100*hi:.1f}%] |')
    (OUT / 'summary-by-subject.md').write_text('\n'.join(md) + '\n')
    print(f'wrote {OUT / "summary-by-subject.md"}')

    # Pooled across S1-S5 by engine
    pooled: dict[str, list[int]] = defaultdict(list)
    for r in all_rows:
        pooled[r['engine_mode']].append(int(r['top1_aligned']))
    md = ['# S1-S5 pooled aligned top-1 by engine', '',
          'Pooled across all five subjects (flask-catalog, listmonk, '
          'healthchecks, umami, petclinic). S1 has the full F1-F10 scope '
          '(100 captures); S2-S5 each cover F1+F2+F3+F6+F7 (15 captures).',
          '',
          '| Engine | n | aligned top-1 | Wilson 95% CI |',
          '|---|---|---|---|']
    for eng, data in sorted(pooled.items()):
        n = len(data); s = sum(data)
        lo, hi = wilson_ci(s, n)
        md.append(f'| {eng} | {n} | {100*s/n:5.1f}% ({s}/{n}) | '
                  f'[{100*lo:.1f}%, {100*hi:.1f}%] |')
    (OUT / 'summary-pooled-s1s5.md').write_text('\n'.join(md) + '\n')
    print(f'wrote {OUT / "summary-pooled-s1s5.md"}')

    print('\n=== Pooled S1-S5 summary ===', flush=True)
    for eng, data in sorted(pooled.items()):
        n = len(data); s = sum(data)
        lo, hi = wilson_ci(s, n)
        print(f'  {eng:25s} n={n:5d} aligned={100*s/n:5.2f}% ({s}/{n})  CI=[{100*lo:.2f}%,{100*hi:.2f}%]')
    return 0


if __name__ == '__main__':
    sys.exit(main())
