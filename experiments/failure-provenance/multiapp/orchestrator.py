#!/usr/bin/env python3
"""Multi-app diagnostic orchestrator — Q1 quality, zero-fault, resume-safe.

Runs all missing diagnostic passes over 340 S2-S5 captures:
  - Rule engine (deterministic, no API):          340 x 5 = 1700
  - LLM-A grounded + freeform (gpt-4o-mini):      340 x 5 x 2 = 3400
  - LLM-B grounded + freeform (cohere-command-a): 340 x 5 x 2 = 3400
  - K8sGPT post-teardown:                         340
  - Kagent post-teardown:                         340

Each output is validated as JSON before being accepted.
Failed/missing files are retried; existing valid outputs are skipped.
"""
import argparse, glob, json, os, random, subprocess, sys, time
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path

ROOT      = Path('/home/azureuser/preview-operator/experiments/failure-provenance')
RESULTS   = ROOT / 'results-multiapp'
DIAGNOSE  = ROOT / 'multiapp' / 'bin' / 'fp-diagnose'
LOG_FILE  = Path('/tmp/orchestrator.log')
STATE_DIR = Path('/tmp/orchestrator-state'); STATE_DIR.mkdir(exist_ok=True)

LLM_A_URL   = 'https://preview-openai-idp.openai.azure.com/openai/deployments/gpt-4o-mini'
LLM_A_MODEL = 'gpt-4o-mini'
LLM_B_URL   = 'https://fp-foundry-133641.cognitiveservices.azure.com/openai/deployments/cohere-command-a'
LLM_B_MODEL = 'cohere-command-a'

LEVELS = ['C1', 'C2', 'C3', 'C4', 'C5']

def log(msg):
    line = f'[{time.strftime("%H:%M:%S")}] {msg}'
    print(line, flush=True)
    with open(LOG_FILE, 'a') as f:
        f.write(line + '\n')

def discover_captures():
    return sorted(glob.glob(str(RESULTS / 's*-*' / 'F*' / 'r*' / 'report.json')))

def is_valid_json(p):
    try:
        if os.path.getsize(p) == 0:
            return False
        with open(p) as f:
            json.load(f)
        return True
    except (FileNotFoundError, OSError, json.JSONDecodeError):
        return False

def run_diag(report, level, mode, engine, key=None):
    """Run fp-diagnose for one (report, level, mode, engine) tuple.
    Output filename follows existing convention:
      rule:  diag-<level>-rule-grounded.json
      llm-a: diag-<level>-llm-<mode>.json
      llm-b: diag-<level>-llm-<mode>-llmb.json
    Returns: ('ok'|'skip'|'fail', out_path, err_msg)
    """
    rdir = os.path.dirname(report)
    if engine == 'rule':
        out = f'{rdir}/diag-{level}-rule-grounded.json'
        cmd = [str(DIAGNOSE), '-report', report, '-level', level,
               '-mode', 'grounded', '-engine', 'rule', '-out', out]
    elif engine == 'llm-a':
        out = f'{rdir}/diag-{level}-llm-{mode}.json'
        cmd = [str(DIAGNOSE), '-report', report, '-level', level,
               '-mode', mode, '-engine', 'llm',
               '-ai-base-url', LLM_A_URL, '-ai-api-key', key,
               '-model', LLM_A_MODEL, '-out', out]
    elif engine == 'llm-b':
        out = f'{rdir}/diag-{level}-llm-{mode}-llmb.json'
        cmd = [str(DIAGNOSE), '-report', report, '-level', level,
               '-mode', mode, '-engine', 'llm',
               '-ai-base-url', LLM_B_URL, '-ai-api-key', key,
               '-model', LLM_B_MODEL, '-out', out]
    else:
        return ('fail', '', f'unknown engine {engine}')

    if is_valid_json(out):
        return ('skip', out, '')

    attempt = 0
    backoff = 4
    last_err = ''
    while attempt < 6:
        try:
            r = subprocess.run(cmd, capture_output=True, text=True, timeout=240)
            if r.returncode == 0 and is_valid_json(out):
                return ('ok', out, '')
            last_err = (r.stderr or r.stdout or '').strip()[:600]
            # rate-limit?
            if '429' in last_err or 'RateLimitReached' in last_err or 'TooManyRequests' in last_err:
                time.sleep(backoff + random.uniform(0, 1.5))
                backoff = min(backoff * 2, 120)
            elif 'context deadline' in last_err or 'timeout' in last_err.lower():
                time.sleep(backoff); backoff = min(backoff * 2, 60)
            else:
                # short transient retry once; otherwise give up early
                if attempt < 1:
                    time.sleep(2)
                else:
                    return ('fail', out, last_err)
        except subprocess.TimeoutExpired:
            last_err = 'TimeoutExpired'
            time.sleep(backoff); backoff = min(backoff * 2, 60)
        attempt += 1
    return ('fail', out, last_err)

def build_jobs(captures, engine, modes):
    jobs = []
    for rep in captures:
        for lvl in LEVELS:
            for mode in modes:
                jobs.append((rep, lvl, mode, engine))
    return jobs

def run_phase(name, jobs, key, max_workers):
    log(f'=== PHASE {name}: {len(jobs)} jobs, workers={max_workers} ===')
    if not jobs:
        log(f'    nothing to do')
        return
    state = STATE_DIR / f'{name}.state'
    total = len(jobs)
    ok = skip = fail = 0
    failures = []
    t0 = time.time()
    with ThreadPoolExecutor(max_workers=max_workers) as ex:
        futs = {ex.submit(run_diag, r, l, m, e, key): (r, l, m, e) for (r, l, m, e) in jobs}
        for i, fut in enumerate(as_completed(futs), 1):
            try:
                status, out, err = fut.result()
            except Exception as ex_:
                status, out, err = 'fail', '', f'exception: {ex_}'
            if status == 'ok':       ok += 1
            elif status == 'skip':   skip += 1
            else:
                fail += 1
                failures.append((out, err))
            if i % 50 == 0 or i == total:
                el = time.time() - t0
                rate = i / el if el > 0 else 0
                eta = (total - i) / rate if rate > 0 else 0
                log(f'    {name} {i}/{total} ok={ok} skip={skip} fail={fail} '
                    f'rate={rate:.1f}/s eta={eta:.0f}s')
    log(f'=== PHASE {name} DONE: ok={ok} skip={skip} fail={fail} '
        f'elapsed={time.time()-t0:.0f}s ===')
    if failures:
        f_log = STATE_DIR / f'{name}.failures'
        with open(f_log, 'w') as f:
            for out, err in failures:
                f.write(f'{out}\n  {err}\n')
        log(f'    failures dumped to {f_log}')

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('phase', choices=['rule', 'llm-a-grounded', 'llm-a-freeform',
                                       'llm-b-grounded', 'llm-b-freeform',
                                       'all-llm-freeform', 'all'])
    ap.add_argument('--key-a', default=os.environ.get('AI_KEY_A', ''))
    ap.add_argument('--key-b', default=os.environ.get('AI_KEY_B', ''))
    ap.add_argument('--workers-rule', type=int, default=8)
    ap.add_argument('--workers-llm-a', type=int, default=10)
    ap.add_argument('--workers-llm-b', type=int, default=3)
    args = ap.parse_args()

    caps = discover_captures()
    log(f'Discovered {len(caps)} captures')

    if args.phase in ('rule', 'all'):
        run_phase('rule', build_jobs(caps, 'rule', ['grounded']), None, args.workers_rule)
    if args.phase in ('llm-a-grounded', 'all'):
        run_phase('llm-a-grounded', build_jobs(caps, 'llm-a', ['grounded']),
                  args.key_a, args.workers_llm_a)
    if args.phase in ('llm-a-freeform', 'all-llm-freeform', 'all'):
        run_phase('llm-a-freeform', build_jobs(caps, 'llm-a', ['freeform']),
                  args.key_a, args.workers_llm_a)
    if args.phase in ('llm-b-grounded', 'all'):
        run_phase('llm-b-grounded', build_jobs(caps, 'llm-b', ['grounded']),
                  args.key_b, args.workers_llm_b)
    if args.phase in ('llm-b-freeform', 'all-llm-freeform', 'all'):
        run_phase('llm-b-freeform', build_jobs(caps, 'llm-b', ['freeform']),
                  args.key_b, args.workers_llm_b)

if __name__ == '__main__':
    main()
