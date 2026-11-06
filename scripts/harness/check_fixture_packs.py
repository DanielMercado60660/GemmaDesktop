"""Offline reference and negative controls for trusted versioned fixtures.

Explicit host execution is required; this does not qualify Docker containment,
model behavior or the app's grading integration. No solver/model is invoked.
Only summary counts are emitted: private assertions are never printed.
"""
from __future__ import annotations
import argparse, json, os, pathlib, subprocess, tempfile

ROOT = pathlib.Path(__file__).resolve().parents[2] / 'internal/harness/packs'

def write(root, files):
    for name, body in files.items():
        path = pathlib.PurePosixPath(name)
        if path.is_absolute() or '..' in path.parts:
            raise ValueError('unsafe fixture path')
        dest = root / name
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text(body)

def run(root, command):
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE='1', GOWORK='off')
    return subprocess.run(command, cwd=root, env=env, text=True,
                          capture_output=True, timeout=60, check=False)

def contracts(root, task):
    payload = json.dumps([c['input'] for c in task['cases']], ensure_ascii=False)
    result = run(root, [*task['gradeCommand'], payload])
    if result.returncode:
        return False
    try:
        return json.loads(result.stdout) == [c['expected'] for c in task['cases']]
    except (ValueError, TypeError):
        return False

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--host', action='store_true', help='execute only repository-owned trusted fixture code locally')
    args = parser.parse_args()
    if not args.host:
        parser.error('explicit --host is required')
    count = coding = readonly = seed_public_failures = 0
    for manifest in sorted(ROOT.glob('*/*/task.json')):
        task = json.loads(manifest.read_text())
        reference = json.loads(manifest.with_name('reference.json').read_text())
        with tempfile.TemporaryDirectory(prefix='gemma-fixture-oracle-') as tmp:
            root = pathlib.Path(tmp)
            write(root, task['public'])
            seed_public_failed = run(root, task['command']).returncode != 0
            seed_public_failures += int(seed_public_failed)
            if task['contract'] == 'read_only':
                if seed_public_failed or reference or not contracts(root, task) or not task.get('expectedAnswer'):
                    raise RuntimeError('read-only control failed')
                readonly += 1
                count += 1
                continue
            if contracts(root, task):
                raise RuntimeError(f'{task["id"]}: unchanged negative control unexpectedly passes')
            tests = {p: b for p, b in reference.items() if p not in task['public']}
            write(root, tests)
            red = run(root, task['discoveryCommand'])
            if red.returncode == 0:
                raise RuntimeError(f'{task["id"]}: regression does not fail on original source')
            write(root, reference)
            if not contracts(root, task):
                raise RuntimeError(f'{task["id"]}: reference violates independent assertions')
            if run(root, task['command']).returncode or run(root, task['discoveryCommand']).returncode:
                raise RuntimeError(f'{task["id"]}: reference public/discovery verification failed')
            # A test-only submission leaves the source unchanged and must fail.
            write(root, task['public'])
            if contracts(root, task) or run(root, task['command']).returncode == 0:
                raise RuntimeError(f'{task["id"]}: test-only negative control passed')
            coding += 1
            count += 1
    print(json.dumps({'fixtures': count, 'codingReferences': coding,
                      'redGreenControls': coding, 'testOnlyNegatives': coding,
                      'readOnlyControls': readonly, 'seedPublicFailures': seed_public_failures,
                      'scope': 'trusted-host-oracles-only'}))

if __name__ == '__main__':
    main()
