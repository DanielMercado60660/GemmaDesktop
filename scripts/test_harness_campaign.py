import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('harness_campaign',Path(__file__).parent/'pilot/harness_campaign.py')
campaign=importlib.util.module_from_spec(spec);spec.loader.exec_module(campaign)


class CampaignOperatorTests(unittest.TestCase):
    def test_admission_accepts_exact_canonical_runtime_spec_digest(self):
        # Frozen completionIdentity uses a canonical algorithm prefix, unlike
        # the bare digest returned by Ollama's catalogue endpoint.
        canonical='sha256:f0fc7e0ae4947d382989b1f57db3d098a86b87c5b6e9523b9561ba81a2a64879'
        frozen={'id':'experiment','status':'ready','trialIds':[],'spec':{'model':'gemma4:26b-mlx','modelDigest':canonical,'fixtureDigest':'pack-digest','baseRevisionId':'base','contextSize':32768,'budgetSeconds':600,'agent':{'maxSteps':40,'finishReserveSeconds':60}}}
        c=campaign.Campaign.__new__(campaign.Campaign)
        c.state={'workspaceId':'workspace','agentId':'agent','packId':'coding-diagnostics-v2','packDigest':'pack-digest','temperature':1.0}
        c.call=lambda *_:frozen
        self.assertEqual(c.admission('duration-units'),frozen)
        self.assertEqual(c.state['initialRevisionId'],'base')
        for incorrect in [canonical.removeprefix('sha256:'),canonical[:-1]+'0']:
            frozen['spec']['modelDigest']=incorrect
            with self.assertRaisesRegex(ValueError,'model or fixture identity drift'):
                c.admission('duration-units')

    def test_diagnostic_counts_by_arm_id_and_redacts_hidden_details(self):
        results=[]
        for task in campaign.TASKS:
            for arm,label in [('arm-A','arm-0'),('arm-B','arm-1')]:
                results.append({'taskId':task,'armId':arm,'arm':label,'repeat':0,'trialStatus':'completed','grade':{'passed':arm=='arm-B','checks':[{'name':'HIDDEN_SENTINEL assertion','passed':arm=='arm-B','error':'HIDDEN_SENTINEL expected value'}]}})
        opaque=campaign.opaque_diagnostic({'id':'diagnostic','status':'completed','results':results},'arm-A','arm-B')
        summary=campaign.diagnostic_summary(opaque)
        self.assertEqual((summary['initialSuccesses'],summary['selectedSuccesses'],summary['results']),(0,5,10))
        self.assertNotIn('HIDDEN_SENTINEL',json.dumps(opaque))
        opaque['results'].pop()
        with self.assertRaisesRegex(RuntimeError,'denominator'):
            campaign.diagnostic_summary(opaque)

    def test_packet_role_and_hash_are_checked_before_submission(self):
        for authored_by,request_hash in [(campaign.AUTHOR,'hash'),(campaign.REVIEWER,'wrong')]:
            c=campaign.Campaign.__new__(campaign.Campaign);c.state={};c.save=lambda:None
            c.call=lambda *_:{'id':'base'}
            c.response=lambda *_:{'response':{'id':'reflection','requestHash':request_hash,'authoredBy':{'kind':'codex','identity':authored_by},'verdict':'approved'},'retainRevisionId':'base'}
            with self.assertRaisesRegex(ValueError,'identity mismatch'):
                c.packet({}, {'index':1,'baseRevisionId':'base'},{'id':'reflection','stage':'reflection','requestHash':'hash','baseRevisionId':'candidate'},[],[])

    def test_immutable_requests_cannot_be_replaced(self):
        with tempfile.TemporaryDirectory() as tmp:
            path=Path(tmp)/'request.json'
            campaign.immutable_json(path,{'evidence':'original'})
            campaign.immutable_json(path,{'evidence':'original'})
            with self.assertRaisesRegex(RuntimeError,'immutable'):
                campaign.immutable_json(path,{'evidence':'changed'})
            self.assertEqual(json.loads(path.read_text()),{'evidence':'original'})

    def test_retention_requires_delivery_and_semantic_grade(self):
        for status,incomplete,passed,finished,infra in [('failed',False,True,True,''),('completed',True,True,True,''),('completed',False,False,True,''),('completed',False,True,False,''),('completed',False,True,True,'interrupted')]:
            with self.subTest(status=status,incomplete=incomplete,passed=passed,finished=finished,infra=infra),tempfile.TemporaryDirectory() as tmp:
                c=campaign.Campaign.__new__(campaign.Campaign);c.out=Path(tmp);c.state={};c.save=lambda:None
                p={'id':'reflection','requestHash':'hash','stage':'reflection','baseRevisionId':'candidate'}
                reply={'id':'reflection','requestHash':'hash','authoredBy':{'kind':'codex','identity':campaign.REVIEWER},'verdict':'approved'}
                c.response=lambda *_:{'response':reply,'retainRevisionId':'candidate'}
                calls=[]
                def call(method,params):
                    calls.append(method)
                    if method=='harness.revisions.get':return {'id':'base'}
                    raise AssertionError('unsafe retention reached submission')
                c.call=call
                with self.assertRaisesRegex(ValueError,'delivery and independent correctness'):
                    c.packet({}, {'index':1,'baseRevisionId':'base'},p,[{'id':'final','label':'final','status':status,'result':{'incomplete':incomplete}}],[{'trialId':'final','passed':passed,'gradingFinished':finished,'infrastructure':infra}])
                self.assertNotIn('harness.packets.submit',calls)

    def test_resume_rejects_changed_binary(self):
        runtime=campaign.g.ROOT/'.runtime';runtime.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=runtime) as tmp:
            out=Path(tmp);binary=out/'binary';binary.write_bytes(b'frozen')
            campaign.Campaign(binary,out)
            state=json.loads((out/'campaign.json').read_text())
            self.assertEqual(len(state['taskOrder']),20)
            self.assertEqual({task:state['taskOrder'].count(task) for task in campaign.TASKS},{task:4 for task in campaign.TASKS})
            self.assertEqual(state['plannedOptimizationTrials'],80)
            self.assertEqual(state['plannedFinalDiagnosticTrials'],10)
            binary.write_bytes(b'drift')
            with self.assertRaisesRegex(ValueError,'frozen binary/operator changed'):
                campaign.Campaign(binary,out,resume=True)


    def test_new_campaign_freezes_default_or_explicit_temperature_and_resume_preserves_it(self):
        for requested in (None, 0.2, 0):
            with self.subTest(temperature=requested), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp).resolve(); binary = root / "binary"; binary.write_bytes(b"synthetic")
                out = root / ".runtime/campaign"
                expected = 1.0 if requested is None else requested
                calls = []

                class App:
                    addr = "127.0.0.1:1"
                    class proc:
                        pid = 0
                    def __init__(self, *args): pass
                    def call(self, method, params=None, **kwargs):
                        calls.append((method, params))
                        if method == "execution.configure": return {}
                        if method == "harness.packs.list":
                            return {"items": [{"id": "coding-diagnostics-v2", "digest": "synthetic-pack", "tasks": [{"id": task, "split": "development", "contract": "coding"} for task in campaign.TASKS]}]}
                        if method == "capability.save": return {"id": "profile"}
                        if method == "workspace.add": return {"id": "workspace"}
                        if method == "agent.save": return {"id": "agent"}
                        if method == "harness.experiments.start":
                            return {"spec": {"modelDigest": campaign.MODEL_DIGEST, "fixtureDigest": "synthetic-pack", "baseRevisionId": "base", "agent": {"temperature": params["temperature"]}}}
                        raise AssertionError(method)

                with patch.object(campaign.g, "ROOT", root), patch.object(campaign.g, "App", App), patch.object(campaign.g.subprocess, "Popen", side_effect=AssertionError("real subprocess")), patch.object(campaign.g.request, "urlopen", side_effect=AssertionError("real HTTP")):
                    c = campaign.Campaign(binary, out, temperature=requested)
                    c.setup()
                    c.admission("duration-units")
                    self.assertEqual(c.state["temperature"], expected)
                    self.assertEqual(next(params for method, params in calls if method == "agent.save")["temperature"], expected)
                    self.assertEqual(next(params for method, params in calls if method == "harness.experiments.start")["temperature"], expected)
                    before = (out / "campaign.json").read_bytes()
                    resumed = campaign.Campaign(binary, out, resume=True)
                    self.assertEqual(resumed.state["temperature"], expected)
                    self.assertEqual((out / "campaign.json").read_bytes(), before)
                    conflicting = 0.2 if expected == 0 else 0
                    with self.assertRaisesRegex(ValueError, "conflicts with the frozen campaign"):
                        campaign.Campaign(binary, out, resume=True, temperature=conflicting)
                    self.assertEqual((out / "campaign.json").read_bytes(), before)
                    self.assertEqual(len([method for method, _ in calls if method == "agent.save"]), 1)
                    # A synthetic old ledger must not be migrated or inferred.
                    legacy = json.loads(before); legacy.pop("temperature")
                    campaign.g.write_json(out / "campaign.json", legacy)
                    legacy_before = (out / "campaign.json").read_bytes()
                    with self.assertRaisesRegex(ValueError, "retained historical operator"):
                        campaign.Campaign(binary, out, resume=True)
                    self.assertEqual((out / "campaign.json").read_bytes(), legacy_before)

    def test_campaign_rejects_invalid_temperature_before_app_start(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(campaign.g, "App", side_effect=AssertionError("real App")):
            root = Path(tmp)
            for invalid in (-0.1, 2.1, float("nan"), float("inf"), True, "1"):
                with self.subTest(temperature=invalid), self.assertRaisesRegex(ValueError, "finite number"):
                    campaign.Campaign(root / "binary", root / ".runtime/campaign", temperature=invalid)


if __name__=='__main__':unittest.main()
