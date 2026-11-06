"""Pinned Harness² core, external Codex packets, Go-only solver effects.

JSON-RPC 2.0 over owned stdin/stdout. No model provider is imported or dispatched.
Stdout is protocol-only; published-core diagnostics go to stderr.
"""
from __future__ import annotations
import contextlib, hashlib, json, os, pathlib, sys, threading
from dataclasses import dataclass

PIN = '6868817518876de8fd170c6a8354daca273b6191'
HERE = pathlib.Path(__file__).resolve()
UPSTREAM = HERE.parents[2] / 'third_party/harness2'
sys.path.insert(0, str(UPSTREAM))

WIRE_OUT = sys.stdout
RPC_LOCK = threading.Lock()
COUNTER = 0

class BatchCheckpoint(BaseException):
    """The Go owner persisted a safe boundary; explicit advance is required."""

def verify_upstream():
    manifest=json.loads((UPSTREAM/'UPSTREAM.json').read_text())
    if manifest['revision'] != PIN: raise RuntimeError('upstream pin mismatch')
    for rel,digest in manifest['files'].items():
        if hashlib.sha256((UPSTREAM/rel).read_bytes()).hexdigest()!=digest:
            raise RuntimeError(f'upstream source changed: {rel}')

# Reject changed donor bytes before importing any executable upstream module.
verify_upstream()
from harness2.core import schedule, tree, runmark, prompts, recursion
from harness2.core.proposer import Reply
from harness2.core.spec import BenchPrompts, BenchSpec, ChannelSpec
from harness2.substrate.base import Install

def rpc(method,params):
    global COUNTER
    with RPC_LOCK:
        COUNTER+=1; ident=COUNTER
        WIRE_OUT.write(json.dumps({'jsonrpc':'2.0','id':ident,'method':method,'params':params})+'\n');WIRE_OUT.flush()
        line=sys.stdin.readline()
        if not line: raise RuntimeError('Go supervisor closed the protocol')
        if len(line)>8*1024*1024:raise RuntimeError('oversized supervisor response')
        response=json.loads(line)
        if response.get('id')!=ident:raise RuntimeError('out-of-order supervisor response')
        if response.get('error'):
            if response['error'].get('code') == -32002:raise BatchCheckpoint(response['error']['message'])
            raise RuntimeError(response['error']['message'])
        return response['result']

@dataclass(frozen=True)
class PacketChannel(ChannelSpec):
    def __post_init__(self):
        if self.kind!='external_packet' or self.model!='codex-guided':
            raise ValueError('only the explicitly external Codex packet channel is supported')

SURFACE=tree.SurfaceSpec(name='gemma_desktop',always_on=('instructions.md','tool_guidance.md','lessons.json','skills.json','workflow.json','delegates.json'),always_on_max=32000,always_on_cap_reason='bounded app-owned harness',skill_dir='approved_skills',skill_name_rule='must_match',trigger_hint='',autotrigger_file='tool_guidance.md',trigger_line='',subagent_requires_mode=True,plugin_dir=None,hooks_file=None)

# The pinned release has no public surface-registration API. This explicit,
# local registry extension is part of our adapter, not a modification of its
# published recursion. Never reuse the Codex/OpenCode surface's executable hooks.
SURFACE_TEXT='''# ALLOWED GEMMA DESKTOP HARNESS SURFACE
Only instructions.md, tool_guidance.md, lessons.json (array of strings), and
skills.json (array of already-approved immutable skill IDs), workflow.json
(one host-approved frozen workflow ID, or an empty string), and delegates.json
(an array of already-approved read-only delegate agent IDs) may change. These
map to declarative Go request fields. They cannot grant tools, change permissions,
change model settings, replace a grader, add executable plugins/scripts, or
modify the task. Submit the edited declarative content through the Codex packet.
Use concise evidence-supported changes. Preserve the complete allowed tree.
'''
prompts._SURFACE_TEXTS[SURFACE.name]=prompts._SurfaceText(
    surface_md=SURFACE_TEXT,
    inv=prompts._INV_TEMPLATE.replace('__LOOP_LEVER__','short tool-use guidance (host enforcement is immutable)'),
    dom=prompts._DOM_TEMPLATE.replace('__TRIGGER_SENTENCE__','Select only existing approved skill IDs; never create executable components.'),
    tts_surface=SURFACE_TEXT)

def encode_content(content):
    return {'instructions.md':content.get('instructions',''),'tool_guidance.md':content.get('toolGuidance',''),'lessons.json':json.dumps(content.get('lessons',[]),sort_keys=True),'skills.json':json.dumps(content.get('skillIds',[]),sort_keys=True),'workflow.json':json.dumps(content.get('workflowId','')),'delegates.json':json.dumps(content.get('delegateAgentIds',[]),sort_keys=True)}
def decode_content(files):
    if set(files)-set(SURFACE.always_on):raise ValueError('unsupported harness component')
    return {'instructions':files.get('instructions.md',''),'toolGuidance':files.get('tool_guidance.md',''),'lessons':json.loads(files.get('lessons.json','[]')),'skillIds':json.loads(files.get('skills.json','[]')),'workflowId':json.loads(files.get('workflow.json','""')),'delegateAgentIds':json.loads(files.get('delegates.json','[]'))}
def fingerprint(files):return hashlib.sha256(json.dumps(files,sort_keys=True).encode()).hexdigest()

class DesktopSubstrate:
    name='gemma-desktop'
    def surface(self):return SURFACE
    def materialise(self,tree_dir,spec,ctx):
        return Install(pathlib.Path(tree_dir),runmark.fingerprint(tree_dir))
    def execute(self,*args,**kwargs):raise RuntimeError('only BenchmarkAdapter.execute may dispatch Go trials')
    def preflight(self):verify_upstream()
    def read_trajectory(self,stream_path,*,obs_trunc,max_chars,args_trunc=400):
        text=pathlib.Path(stream_path).read_text()
        return text if len(text)<=max_chars else text[:max_chars]+'\n[Explicit trajectory truncation]'

class Adapter:
    def __init__(self,init):
        self.init=init;self.root=pathlib.Path.cwd();self.substrate=DesktopSubstrate();self.current_task='';self.trials={};self.last_revision=init['baseline']['id'];self.completed=0
        ch=PacketChannel('external_packet','codex-guided')
        self.spec=BenchSpec(name='gemma-local-fixtures',runs_root=self.root/'runs',install_mode='workspace_config',prompts=BenchPrompts(work='repair a small local service according to its public README and preserve its CLI contract',inv_levers='instructions and tool-use guidance',dom_levers='domain-specific guidance and approved skill selection'),channels={'proposer':ch,'selector':ch},leak_globs=('grader/**','reference.json','evaluation.json'),allow_subagents=False)
        self.by_id={t['id']:t for t in init['tasks']}
        self.revisions={r['id']:encode_content(r['content']) for r in init.get('revisions',[init['baseline']])}
        self.revisions.setdefault(init['baseline']['id'],encode_content(init['baseline']['content']))
        self.bindings={}
        for binding in init.get('revisionBindings',[]):self.bind_stage(binding)
        for tid in self.by_id:
            self.bind_stage({'taskId':tid,'label':'base','revisionId':init['baseline']['id']})
        tree.write_tree(SURFACE,encode_content(init['baseline']['content']),self.base_harness(),stubs=False)
        for tid,t in self.by_id.items():
            source=self.task_files(tid);source.mkdir(parents=True,exist_ok=True)
            for rel,body in t['public'].items():
                p=pathlib.PurePosixPath(rel)
                if p.is_absolute() or '..' in p.parts:raise ValueError('unsafe source path')
                dest=source/rel;dest.parent.mkdir(parents=True,exist_ok=True);dest.write_text(body)
        for result in init.get('trials',[]):
            self.trials[result['id']]=result
        self.completed=len(self.trials)
        for tid in self.by_id:
            for result in self.trials.values():
                if result['taskId']==tid and result['label']=='final':self.last_revision=result['revisionId']
    def tasks(self,split='all'):return list(self.by_id)
    def domain_of(self,task_id):return self.by_id[task_id]['stack']
    def task_prompt(self,task_id):return self.by_id[task_id]['prompt']
    def task_files(self,task_id):return self.root/'sources'/task_id
    def base_harness(self):return self.root/'base'
    def bind_stage(self,binding):
        task,label,revision=binding['taskId'],binding['label'],binding['revisionId']
        if task not in self.by_id or label not in ('base','inv','dom','final') or revision not in self.revisions:
            raise ValueError('unknown task/stage/revision binding')
        if label=='base' and revision!=self.init['baseline']['id']:raise ValueError('baseline revision cannot be rebound')
        key=(task,label)
        if key in self.bindings and self.bindings[key]!=revision:raise ValueError('immutable task/stage binding changed')
        self.bindings[key]=revision
    def validate_tree(self,files):decode_content(files)
    def execute(self,task_id,harness_dir,run_id,*,timeout=None):
        self.current_task=task_id
        tail=run_id.rsplit('/',1)[-1];label='base' if tail.startswith('base') else 'inv' if tail.startswith('inv') else 'dom' if tail.startswith('dom') else 'final'
        files=tree.read_tree(SURFACE,harness_dir or self.base_harness());rev=self.bindings.get((task_id,label))
        if not rev or fingerprint(files)!=fingerprint(self.revisions[rev]):raise ValueError('task/stage harness content differs from frozen revision')
        result=rpc('solve',{'taskId':task_id,'label':label,'revisionId':rev});self.trials[result['id']]=result
        dest=self.root/'executions'/result['id'];dest.mkdir(parents=True,exist_ok=True)
        (dest/'result.json').write_text(json.dumps(result));(dest/'answer.txt').write_text(result['result'].get('content',''))
        lines=[]
        for call in (result['result'].get('toolCalls') or []):
            lines.append('[ACTION] '+call['tool']+' '+json.dumps(call.get('arguments',{})))
            lines.append(('[ERROR] ' if call.get('error') else '[OBSERVATION] ')+(call.get('error') or call.get('content','')))
        (dest/'trajectory.md').write_text('\n'.join(lines));runmark.mark_complete(dest,run_id=run_id,harness_dir=harness_dir,inputs=runmark.inputs_fingerprint(self.task_prompt(task_id),self.task_files(task_id)))
        self.last_revision=rev if label=='final' else self.last_revision;self.completed=len(self.trials)
        return dest
    def delivered(self,run_dir):
        d=json.loads((pathlib.Path(run_dir)/'result.json').read_text());return d['status']=='completed' and bool(d['result'].get('content'))
    def artifacts_intact(self,run_dir):
        root=pathlib.Path(run_dir)
        try:
            result=json.loads((root/'result.json').read_text())
            if result != self.trials.get(result.get('id')):return False
            task=result['taskId']
            expected=runmark.inputs_fingerprint(self.task_prompt(task),self.task_files(task))
            mark=runmark.read_mark(root)
            return bool(mark and mark.get('inputs')==expected and (root/'answer.txt').read_text()==result['result'].get('content',''))
        except (OSError,ValueError,KeyError):return False
    def trajectory(self,run_dir,*,obs_trunc):return (pathlib.Path(run_dir)/'trajectory.md').read_text()
    def deliverable_files(self,run_dir):return {'answer.txt':self.deliverable_text(run_dir)}
    def deliverable_text(self,run_dir):return (pathlib.Path(run_dir)/'answer.txt').read_text()
    def score(self,*args):raise RuntimeError('grading is external to the optimizer; no Gemma self-grading')

class ExternalPacketProposer:
    def __init__(self,adapter):self.adapter=adapter;self.spec=adapter.spec.channels['proposer'];self._calls=[]
    def call(self,mission,context_dir,label,**kwargs):
        stage='reflection' if label.startswith('experience.') else 'compose' if label.startswith(('agg','c')) else 'proposal_inv' if label.startswith('inv') else 'proposal_dom' if label.startswith('dom') else None
        if stage is None:raise RuntimeError('k=1 has no selection call; unsupported stage '+label)
        context=pathlib.Path(context_dir);files={}
        for p in sorted(context.rglob('*')):
            if p.is_symlink():raise ValueError('symlink in packet context')
            if p.is_file():files[str(p.relative_to(context))]=p.read_text()
        task=self.adapter.current_task;evidence=[tid for tid,t in self.adapter.trials.items() if t['taskId']==task]
        packet=rpc('packet',{'taskId':task,'stage':stage,'baseRevisionId':self.adapter.last_revision if stage=='reflection' else self.adapter.init['baseline']['id'],'mission':mission,'evidence':evidence,'context':files})
        response=packet['response'];workspace=None;text=response.get('notes','')
        if stage=='reflection':
            if response['verdict']!='approved':raise RuntimeError('independent Codex reflection rejected')
            lessons=response.get('lessons',[])
            text='NO_ENTRY' if not lessons else '<experience>\ncue: '+lessons[0]['cue']+'\nlesson: '+lessons[0]['guidance']+'\nevidence: '+', '.join(lessons[0]['evidence'])+'\n</experience>'
        else:
            workspace=context/'harness';files=encode_content(response['content']);tree.write_tree(SURFACE,files,workspace,stubs=False)
            self.adapter.revisions[packet['revisionId']]=files
            rollout={'proposal_inv':'inv','proposal_dom':'dom','compose':'final'}[stage]
            self.adapter.bind_stage({'taskId':task,'label':rollout,'revisionId':packet['revisionId']})
            text='<notes>'+text+'</notes>'
        reply=Reply(label=label,text=text,ok=True,attempts=1,workspace=workspace,channel='external_packet');self._calls.append(reply);return reply

@contextlib.contextmanager
def serial_core_phases():
    """Use the pinned recursion's own width controls without editing its source.

    schedule.run_task does not expose those controls. Keep agg semantics: both
    proposals use the same baseline; only dispatch order/concurrency changes.
    This sidecar owns one schedule at a time and restores the hook on exit.
    """
    original = recursion.run_mode
    def dispatch(*args, **kwargs):
        kwargs['propose_workers'] = 1
        kwargs['rollout_workers'] = 1
        return original(*args, **kwargs)
    recursion.run_mode = dispatch
    try:
        yield
    finally:
        recursion.run_mode = original

@contextlib.contextmanager
def strict_core_cache():
    """Do not inherit the donor's permissive missing-input legacy cache policy.

    No published source is edited. This owned sidecar adds stricter admission,
    just as serial_core_phases narrows its concurrency. Go remains final owner
    of trial input/output identities and never replays uncertain solver effects.
    """
    original_match=runmark._inputs_match
    original_resume=schedule._resume
    def match(marker,inputs):
        return inputs is None or marker.get('inputs')==inputs
    def resume(summary_path,*args,**kwargs):
        try:
            if not json.loads(pathlib.Path(summary_path).read_text()).get('inputs'):return False
        except (OSError,ValueError):return False
        return original_resume(summary_path,*args,**kwargs)
    runmark._inputs_match=match;schedule._resume=resume
    try:yield
    finally:runmark._inputs_match=original_match;schedule._resume=original_resume

def main():
    verify_upstream()
    init=json.loads(sys.stdin.readline())
    if init.get('upstreamRevision')!=PIN:raise RuntimeError('supervisor pin mismatch')
    adapter=Adapter(init);proposer=ExternalPacketProposer(adapter)
    # Both injection points are explicit: no provider-backed fallback is reachable.
    try:
        with contextlib.redirect_stdout(sys.stderr), serial_core_phases(), strict_core_cache():
            for tid in adapter.tasks():
                adapter.current_task=tid
                library=__import__('harness2.core.library',fromlist=['Library']).Library(root=adapter.root/'library',domain=adapter.domain_of(tid),surface=SURFACE)
                result=schedule.run_task(tid,1,adapter=adapter,prop=proposer,selector_prop=proposer,runs_root=adapter.spec.runs_root,modes=('agg',),library=library)
                if result.status!='completed':
                    # Core uses 'ok' for merged success; preserve actual diagnostics.
                    if not result.modes.get('agg',{}).get('ok') or result.errors:raise RuntimeError(json.dumps(result.to_dict()))
    except BatchCheckpoint:
        return
    if init.get('fixturePackId') and len(adapter.tasks()) > 1:
        packet=rpc('compose_all',{})
        if not packet.get('revisionId'):raise RuntimeError('cross-task composition lacks a frozen revision')
        adapter.last_revision=packet['revisionId']
    stages=3*len(adapter.tasks())+int(bool(init.get('fixturePackId')) and len(adapter.tasks())>1)
    rpc('finish',{'candidateId':adapter.last_revision,'rollouts':adapter.completed,'stages':stages})

if __name__=='__main__':main()
