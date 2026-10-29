#!/usr/bin/env python3
"""Opt-in local runtime capacity probe; not a coding or long-context quality eval."""
import argparse
import hashlib
import json
import random
import subprocess
import threading
import time
import urllib.request
from pathlib import Path


def post(path, value, timeout=600):
    return urllib.request.urlopen(urllib.request.Request(
        'http://127.0.0.1:11434' + path,
        data=json.dumps(value).encode(),
        headers={'Content-Type': 'application/json'}), timeout=timeout)


def memory_sample():
    # Read-only VM counters; RSS includes mapped/shared pages and is not peak
    # physical GPU allocation. Keep raw counter evidence instead of equating them.
    return {'elapsedSeconds': round(time.monotonic() - started, 3),
            'vmStat': subprocess.check_output(['vm_stat'], text=True),
            'swap': subprocess.check_output(['sysctl', 'vm.swapusage'], text=True),
            'pressure': subprocess.check_output(['memory_pressure', '-Q'], text=True)}


parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--model', default='gemma4:26b-mlx')
parser.add_argument('--context', type=int, default=65536)
parser.add_argument('--rows', type=int, default=3600)
parser.add_argument('--runner-port', type=int, help='Observed private MLX runner port, for exact preflight token counting')
parser.add_argument('--output', type=Path, required=True)
args = parser.parse_args()
if args.output.exists():
    parser.error('Refusing to overwrite evidence')
if not 1 <= args.rows <= 6000 or args.context not in (32768, 65536, 131072):
    parser.error('Unsupported bounded probe size')

words = 'amber birch cedar dune elm fern granite harbor iris juniper kelp lake maple north oak pine quartz ridge stone tide umber violet willow xylem yellow zinc'.split()
rng = random.Random(27102026)
markers = ['FIELDNOTE_START_AMBER_73', 'FIELDNOTE_MIDDLE_CEDAR_41', 'FIELDNOTE_END_IRIS_29']
rows = []
for i in range(args.rows):
    rows.append(f'Record {i:05d}: ' + ' '.join(rng.choices(words, k=5)) + '.')
for i, marker in zip((0, args.rows // 2, args.rows - 1), markers):
    rows[i] += ' Audit marker: ' + marker
prompt = ('Read this synthetic audit ledger. Retain each Audit marker exactly.\n'
          + '\n'.join(rows)
          + '\nReturn only the three Audit markers in their original order, one per line.')
request = {'model': args.model, 'stream': True, 'think': False, 'truncate': False, 'shift': False,
           'messages': [{'role': 'user', 'content': prompt}],
           'options': {'num_ctx': args.context, 'num_predict': 512, 'temperature': 0}}
encoded = json.dumps(request).encode()
if len(encoded) > 512 * 1024:
    parser.error('Probe exceeds application serialized text cap')
started = time.monotonic()
report = {'scope': 'Single raw Ollama capacity/recall probe; no ADK or tools. Not full-window or coding qualification.',
          'model': args.model, 'requestedContext': args.context, 'rows': args.rows,
          'requestBytes': len(encoded), 'requestSHA256': hashlib.sha256(encoded).hexdigest(),
          'promptCharacters': len(prompt), 'outputLimit': 512, 'thinking': False,
          'runtime': json.load(urllib.request.urlopen('http://127.0.0.1:11434/api/version')),
          'samples': [], 'complete': False}
stop = threading.Event()


def monitor():
    while not stop.is_set():
        try:
            report['samples'].append(memory_sample())
        except Exception as exc:
            report.setdefault('monitorErrors', []).append(str(exc))
        stop.wait(5)


thread = threading.Thread(target=monitor, daemon=True)
thread.start()
content = ''
try:
    if args.runner_port:
        # Qualification-only private APIs, verified against Ollama 0.40.11.
        # Render-only schedules the model but does not decode a response.
        with post('/api/chat', dict(request, stream=False, _debug_render_only=True)) as response:
            rendered = json.load(response)['_debug_info']['rendered_template']
        token_request = urllib.request.Request(
            f'http://127.0.0.1:{args.runner_port}/v1/tokenize',
            data=rendered.encode(), headers={'Content-Type': 'text/plain'})
        with urllib.request.urlopen(token_request, timeout=60) as response:
            tokens = json.load(response)
        if not isinstance(tokens, list):
            raise RuntimeError('Unexpected private tokenizer response')
        report['preflightPromptTokens'] = len(tokens)
        if len(tokens) + 512 > args.context:
            raise RuntimeError('Probe exceeds requested context with output reserve; reduce rows')
    with post('/api/chat', request) as response:
        for line in response:
            chunk = json.loads(line)
            if chunk.get('error'):
                raise RuntimeError(chunk['error'])
            content += chunk.get('message', {}).get('content', '')
            if len(content) > 4096:
                raise RuntimeError('Probe output unexpectedly large')
            if chunk.get('done'):
                report['complete'] = True
                report['metrics'] = {k: v for k, v in chunk.items() if k != 'message'}
                break
except Exception as exc:
    report['error'] = str(exc)
finally:
    stop.set()
    thread.join(10)
    report['elapsedSeconds'] = round(time.monotonic() - started, 3)
    report['answer'] = content
    report['markersRecalled'] = [marker in content for marker in markers]
    report['samples'].append(memory_sample())
    args.output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({k: report.get(k) for k in ['complete', 'elapsedSeconds', 'markersRecalled', 'metrics', 'error']}))
