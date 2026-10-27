#!/usr/bin/env python3
"""Exercise the running application against a real local model, never a mock.

Only approves writes inside this generated fixture and the prescribed Python
test command. All evidence is saved; execution failure raises rather than passes.
"""
import json, os, pathlib, subprocess, sys, time, urllib.request, urllib.error
ROOT = pathlib.Path(__file__).resolve().parents[1]
BASE = os.environ.get('GEMMA_TEST_URL', 'http://127.0.0.1:8787')
WORK = ROOT / '.runtime' / ('e2e-' + time.strftime('%Y%m%d-%H%M%S'))
WORK.mkdir(parents=True)
(WORK/'calculator.py').write_text('def add(a, b):\n    return a - b\n')
(WORK/'test_calculator.py').write_text('import unittest\nfrom calculator import add\n\nclass ArithmeticTests(unittest.TestCase):\n    def test_positive(self):\n        self.assertEqual(add(3, 4), 7)\n    def test_negative(self):\n        self.assertEqual(add(-3, -4), -7)\n    def test_mixed(self):\n        self.assertEqual(add(-3, 4), 1)\n\nif __name__ == "__main__":\n    unittest.main()\n')
def api(method, params=None):
    data=json.dumps({'method':method,'params':params or {}}).encode()
    req=urllib.request.Request(BASE+'/api/call',data=data,headers={'Content-Type':'application/json'})
    try:
        with urllib.request.urlopen(req,timeout=120) as r: return json.load(r)
    except urllib.error.HTTPError as e:
        raise RuntimeError(e.read().decode()) from e
before=subprocess.run([sys.executable,'-m','unittest','-v'],cwd=WORK,capture_output=True,text=True)
assert before.returncode != 0, 'Fixture must fail before the agent works'
workspace=api('workspace.add',{'path':str(WORK),'name':'Integration · arithmetic repair'})
session=api('session.create',{'workspaceId':workspace['id'],'title':'Repair failing arithmetic tests'})
run=api('run.start',{'sessionId':session['id'],'prompt':'Inspect calculator.py and test_calculator.py. Diagnose and fix the failing addition function, preserving all tests. Use the governed file tools for editing and run the tests with run_command using executable '+sys.executable+' and args ["-m", "unittest", "-v"]. Report the concrete change and the observed test result. Do not modify the tests.'})
deadline=time.time()+1200
decisions=[]
while time.time()<deadline:
    state=api('state')
    for a in state['approvals']:
        if a.get('runId')!=run['id'] or a['status']!='pending': continue
        args=a.get('arguments') or {}
        tool=a['tool']
        allow=False
        if tool in ('file.write','file.edit','file.patch','write_file','edit_file','apply_patch'):
            allow=args.get('path')=='calculator.py'
        if tool in ('shell.run','run_command'):
            allow=args.get('executable')==sys.executable and args.get('args')==['-m','unittest','-v']
        decisions.append({'tool':tool,'arguments':args,'allowed':allow})
        api('approval.respond',{'id':a['id'],'allow':allow})
    current=next(r for r in state['runs'] if r['id']==run['id'])
    if current['status'] in ('completed','failed','cancelled','interrupted'): break
    time.sleep(.7)
else:
    api('run.cancel',{'id':run['id']});raise RuntimeError('Live agent exceeded test deadline')
after=subprocess.run([sys.executable,'-m','unittest','-v'],cwd=WORK,capture_output=True,text=True)
evidence={'createdAt':time.strftime('%Y-%m-%dT%H:%M:%S%z'),'workspace':str(WORK),'run':current,'runtime':state['runtime'],'models':state['models'],'decisions':decisions,'before':{'exitCode':before.returncode,'output':before.stdout+before.stderr},'after':{'exitCode':after.returncode,'output':after.stdout+after.stderr},'conversation':api('session.get',{'id':session['id']}),'artifacts':[a for a in state['artifacts'] if a.get('runId')==run['id']]}
out=ROOT/'docs/evidence';out.mkdir(parents=True,exist_ok=True)
(out/'live-coding-task.json').write_text(json.dumps(evidence,indent=2))
print(json.dumps({'status':current['status'],'testsExitCode':after.returncode,'runId':run['id'],'evidence':str(out/'live-coding-task.json')},indent=2))
assert current['status']=='completed', current
assert after.returncode==0, after.stdout+after.stderr
assert evidence['artifacts'], 'Expected a recorded file artifact'
