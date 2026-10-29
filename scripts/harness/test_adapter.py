"""Offline adapter contract tests; never invoke the solver or a model provider."""
import importlib.util,json,pathlib,shutil,subprocess,sys,tempfile,unittest
HERE=pathlib.Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('desktop_adapter',HERE/'upstream_adapter.py')
module=importlib.util.module_from_spec(spec);sys.modules[spec.name]=module;spec.loader.exec_module(module)
class AdapterTests(unittest.TestCase):
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
if __name__=='__main__':unittest.main()
