#!/usr/bin/env python3
"""Real two-agent workflow, human checkpoint, and frozen-input evaluation test."""
import json, os, pathlib, time, urllib.request, urllib.error
ROOT=pathlib.Path(__file__).resolve().parents[1]
BASE=os.environ.get('GEMMA_TEST_URL','http://127.0.0.1:8787')
def api(method,params=None):
    req=urllib.request.Request(BASE+'/api/call',data=json.dumps({'method':method,'params':params or {}}).encode(),headers={'Content-Type':'application/json'})
    try:
        with urllib.request.urlopen(req,timeout=120) as r:return json.load(r)
    except urllib.error.HTTPError as e:raise RuntimeError(e.read().decode()) from e
work=ROOT/'.runtime'/('workflow-'+time.strftime('%Y%m%d-%H%M%S'));work.mkdir(parents=True)
(work/'project.txt').write_text('Project: Lantern. Purpose: a local notes application. Constraints: offline operation, plain-text export, and keyboard navigation. Target users: researchers.\n')
w=api('workspace.add',{'path':str(work),'name':'Integration · workflow review'})
a=api('agent.save',{'name':'Project analyst','instructions':'Read the supplied project file with read_file. Give a concise factual analysis. Never invent requirements.','model':'gemma4:12b-mlx','maxSteps':6,'mode':'read-only'})
b=api('agent.save',{'name':'Independent reviewer','instructions':'Inspect the evidence with read_file and review the analysis. Be concise and distinguish facts from suggestions.','model':'gemma4:12b-mlx','maxSteps':6,'mode':'read-only'})
wf=api('workflow.save',{'name':'Analyze · approve · review','description':'Live integration verification with an explicit human checkpoint.','nodes':[{'id':'analyst','type':'agent','label':'Analyze project','agentId':a['id'],'prompt':'Read project.txt and summarize its three constraints in one sentence.','position':{'x':50,'y':70}},{'id':'approval','type':'approval','label':'Review permission','prompt':'Approve the independent review of this generated test project.','position':{'x':340,'y':70}},{'id':'reviewer','type':'agent','label':'Independent review','agentId':b['id'],'prompt':'Read project.txt, check the analysis, and suggest one test for offline operation. Reply in at most three sentences.','position':{'x':630,'y':70}}],'edges':[{'id':'e1','source':'analyst','target':'approval'},{'id':'e2','source':'approval','target':'reviewer'}]})
s=api('session.create',{'workspaceId':w['id'],'title':'A workflow with a human checkpoint'})
run=api('workflow.run',{'id':wf['id'],'sessionId':s['id'],'prompt':'Analyze the project, request approval, then perform an independent review.'})
deadline=time.time()+900;approved=0
while time.time()<deadline:
    state=api('state')
    for p in state['approvals']:
        if p.get('runId')==run['id'] and p['status']=='pending':
            allow=p['tool']=='workflow.approval';api('approval.respond',{'id':p['id'],'allow':allow});approved+=int(allow)
    result=next(r for r in state['runs'] if r['id']==run['id'])
    if result['status'] in ('completed','failed','cancelled','interrupted'):break
    time.sleep(.7)
else:api('run.cancel',{'id':run['id']});raise RuntimeError('Workflow timed out')
evidence={'run':result,'workflow':wf,'approvalCount':approved,'conversation':api('session.get',{'id':s['id']})}
out=ROOT/'docs/evidence';out.mkdir(exist_ok=True)
(out/'live-workflow.json').write_text(json.dumps(evidence,indent=2))
assert result['status']=='completed',result
assert approved==1,approved
experiment=api('eval.start',{'workspaceId':w['id'],'baselineAgentId':a['id'],'candidateAgentId':b['id'],'tasks':['Read project.txt exactly once. Then answer from that evidence: list the three explicit constraints and propose one NEW test for offline operation. Do not search for existing tests; none are needed for this analysis.']})
deadline=time.time()+600
while time.time()<deadline:
    state=api('state');result=next(e for e in state['experiments'] if e['id']==experiment['id'])
    if result['status']!='running':break
    time.sleep(1)
else:api('eval.cancel',{'id':experiment['id']});raise RuntimeError('Evaluation timed out')
(out/'live-evaluation.json').write_text(json.dumps(result,indent=2))
assert result['status']=='completed',result
assert len(result['results'])==2 and all(r['status']=='completed' for r in result['results']),result
print(json.dumps({'workflow':'passed','approvalCount':approved,'evaluation':'passed','comparisons':len(result['results'])},indent=2))
