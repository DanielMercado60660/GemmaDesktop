"""Offline adapter contract tests; never invoke the solver or a model provider."""
import importlib.util,io,json,os,pathlib,shutil,subprocess,sys,tempfile,unittest
from unittest.mock import patch
HERE=pathlib.Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('desktop_adapter',HERE/'upstream_adapter.py')
module=importlib.util.module_from_spec(spec);sys.modules[spec.name]=module;spec.loader.exec_module(module)
class AdapterTests(unittest.TestCase):
 def test_identical_content_restores_task_stage_identity_and_checks_exact_tree(self):
  same={'instructions':'same','toolGuidance':'same','lessons':[],'skillIds':[]}
  revisions=[{'id':rid,'content':same} for rid in ['base-id','shared-proposal-id','compose-id']]
  tasks=[{'id':tid,'stack':'python','prompt':'public prompt','public':{'code.py':'public source'}} for tid in ['task-a','task-b']]
  bindings=[{'taskId':tid,'label':label,'revisionId':rid} for tid in ['task-a','task-b'] for label,rid in [('base','base-id'),('inv','shared-proposal-id'),('dom','shared-proposal-id'),('final','compose-id')]]
  init={'baseline':revisions[0],'revisions':revisions,'tasks':tasks,'revisionBindings':bindings}
  old=os.getcwd()
  try:
   with tempfile.TemporaryDirectory() as tmp:
    os.chdir(tmp);adapter=module.Adapter(init);calls=[]
    def solve(method,params):
     self.assertEqual(method,'solve');calls.append(params)
     return {'id':f'trial-{len(calls)}','taskId':params['taskId'],'label':params['label'],'revisionId':params['revisionId'],'status':'completed','result':{'content':'public answer'}}
    with patch.object(module,'rpc',side_effect=solve):
     for tid in ['task-a','task-b']:
      for label in ['base','inv','dom','final']:adapter.execute(tid,adapter.base_harness(),f'task/{label}-0')
     self.assertEqual([c['revisionId'] for c in calls],['base-id','shared-proposal-id','shared-proposal-id','compose-id']*2)
     self.assertEqual(len(adapter.trials),8)
     (adapter.base_harness()/'instructions.md').write_text('changed content')
     with self.assertRaisesRegex(ValueError,'content differs'):adapter.execute('task-a',adapter.base_harness(),'task/base-0')
     self.assertEqual(len(calls),8)
    with self.assertRaisesRegex(ValueError,'baseline revision cannot be rebound'):adapter.bind_stage({'taskId':'task-a','label':'base','revisionId':'compose-id'})
    with self.assertRaisesRegex(ValueError,'immutable task/stage'):adapter.bind_stage({'taskId':'task-a','label':'inv','revisionId':'compose-id'})
    with self.assertRaisesRegex(ValueError,'unknown task/stage'):adapter.bind_stage({'taskId':'unknown','label':'inv','revisionId':'compose-id'})
  finally:os.chdir(old)
 def test_declarative_choices_roundtrip(self):
  content={'instructions':'work','toolGuidance':'read','lessons':['observe'],'skillIds':['digest'],'workflowId':'review','delegateAgentIds':['reader']}
  self.assertEqual(module.decode_content(module.encode_content(content)),content)
  with self.assertRaises(ValueError):module.decode_content({'shell.sh':'forbidden'})
 def test_pin_checked_before_import(self):
  with tempfile.TemporaryDirectory() as tmp:
   root=pathlib.Path(tmp);scripts=root/'scripts/harness';scripts.mkdir(parents=True)
   shutil.copy(HERE/'upstream_adapter.py',scripts/'upstream_adapter.py')
   donor=root/'third_party/harness2';shutil.copytree(module.UPSTREAM,donor)
   target=donor/'harness2/core/__init__.py';marker=root/'executed'
   target.write_text(target.read_text()+f'\nopen({str(marker)!r},"w").write("BAD")\n')
   result=subprocess.run([sys.executable,'-I','-B',str(scripts/'upstream_adapter.py')],capture_output=True,text=True)
   self.assertNotEqual(result.returncode,0);self.assertIn('upstream source changed',result.stderr);self.assertFalse(marker.exists())
 def test_serial_core_controls_and_restore_after_error(self):
  original=module.recursion.run_mode;seen=[]
  def fake(*args,**kwargs):seen.append((args,kwargs));return 'published-core-result'
  module.recursion.run_mode=fake
  try:
   with self.assertRaisesRegex(RuntimeError,'stop'):
    with module.serial_core_phases():
     self.assertEqual(module.recursion.run_mode('agg','task',1,propose_workers=7,rollout_workers=7),'published-core-result')
     raise RuntimeError('stop')
   self.assertIs(module.recursion.run_mode,fake)
   self.assertEqual(seen,[(('agg','task',1),{'propose_workers':1,'rollout_workers':1})])
  finally:module.recursion.run_mode=original
 def test_no_provider_fallback(self):
  self.assertFalse(hasattr(module,'ProviderProposer'))
  with self.assertRaises(ValueError):module.PacketChannel('provider','gemma')
  with self.assertRaises(RuntimeError):module.DesktopSubstrate().execute()
 def test_strict_cache_rejects_missing_input_identity_and_restores_core(self):
  original_match=module.runmark._inputs_match;original_resume=module.schedule._resume
  with tempfile.TemporaryDirectory() as tmp:
   summary=pathlib.Path(tmp)/'summary.json';summary.write_text(json.dumps({'status':'ok'}))
   with module.strict_core_cache():
    self.assertFalse(module.runmark._inputs_match({},'current-inputs'))
    self.assertFalse(module.runmark._inputs_match({'inputs':'old-inputs'},'current-inputs'))
    self.assertTrue(module.runmark._inputs_match({'inputs':'current-inputs'},'current-inputs'))
    self.assertFalse(module.schedule._resume(summary))
   self.assertIs(module.runmark._inputs_match,original_match)
   self.assertIs(module.schedule._resume,original_resume)
 def test_execution_cache_binds_go_record_source_prompt_and_answer(self):
  with tempfile.TemporaryDirectory() as tmp:
   root=pathlib.Path(tmp);source=root/'source';source.mkdir();(source/'code.py').write_text('original')
   run=root/'run';run.mkdir();result={'id':'trial','taskId':'task','status':'completed','result':{'content':'observed answer'}}
   adapter=module.Adapter.__new__(module.Adapter);adapter.trials={'trial':result};adapter.task_prompt=lambda _: 'public prompt';adapter.task_files=lambda _:source
   (run/'result.json').write_text(json.dumps(result));(run/'answer.txt').write_text('observed answer')
   module.runmark.mark_complete(run,run_id='trial',harness_dir=None,inputs=module.runmark.inputs_fingerprint('public prompt',source))
   self.assertTrue(adapter.artifacts_intact(run))
   (run/'answer.txt').write_text('changed');self.assertFalse(adapter.artifacts_intact(run));(run/'answer.txt').write_text('observed answer')
   (source/'code.py').write_text('different source');self.assertFalse(adapter.artifacts_intact(run));(source/'code.py').write_text('original')
   adapter.task_prompt=lambda _: 'changed prompt';self.assertFalse(adapter.artifacts_intact(run));adapter.task_prompt=lambda _: 'public prompt'
   changed=dict(result,status='failed');(run/'result.json').write_text(json.dumps(changed));self.assertFalse(adapter.artifacts_intact(run))
 def test_checkpoint_rpc_escapes_upstream_exception_handlers(self):
  wire,stdin,counter=module.WIRE_OUT,sys.stdin,module.COUNTER
  try:
   module.WIRE_OUT=io.StringIO();module.COUNTER=0;sys.stdin=io.StringIO(json.dumps({'id':1,'error':{'code':-32002,'message':'persisted checkpoint'}})+'\n')
   self.assertFalse(issubclass(module.BatchCheckpoint,Exception))
   with self.assertRaises(module.BatchCheckpoint):module.rpc('solve',{'taskId':'task'})
   request=json.loads(module.WIRE_OUT.getvalue());self.assertEqual(request['method'],'solve');self.assertEqual(request['id'],1)
   sys.stdin=io.StringIO(json.dumps({'id':99,'result':{}})+'\n')
   with self.assertRaisesRegex(RuntimeError,'out-of-order'):module.rpc('solve',{})
  finally:module.WIRE_OUT,sys.stdin,module.COUNTER=wire,stdin,counter
if __name__=='__main__':unittest.main()
