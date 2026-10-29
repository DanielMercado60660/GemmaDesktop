#!/usr/bin/env python3
"""Actual local visual-video workflow qualification against a running isolated app.

Inputs are a pre-generated synthetic clip and an evaluator-only manifest.
This script does not modify application code or model answers.
"""
import argparse
import hashlib
import json
from pathlib import Path
import time
from datetime import datetime, timezone
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--url', default='http://127.0.0.1:8787')
    parser.add_argument('--manifest', type=Path, default=ROOT/'.runtime/video-evaluation/ground-truth.json')
    parser.add_argument('--model', default='gemma4:26b-mlx')
    parser.add_argument('--report', type=Path, default=ROOT/'docs/evidence/video-workflow-live.json')
    options = parser.parse_args()
    if options.url not in ('http://127.0.0.1:8787', 'http://127.0.0.1:8788'):
        raise ValueError('Only the isolated local test servers are supported')
    manifest = json.loads(options.manifest.read_text())
    workspace = Path(manifest['workspace'])
    source = workspace/manifest['clip']
    assert sha(source) == manifest['clipSHA256']
    harness_hash = sha(Path(__file__))
    started = time.monotonic()
    report = {'createdAt':datetime.now(timezone.utc).isoformat(), 'scope':'Actual local visual frame-RAG, synthetic six-scene clip; separate from general video or audio capability.', 'manifest':manifest,'model':options.model,'harnessSHA256':harness_hash,'answers':[],'jobs':[]}
    destination = options.report
    if destination.exists():
        raise FileExistsError('Preserve the earlier report; choose a new --report path')

    def save():
        temporary = destination.with_suffix('.tmp')
        temporary.write_text(json.dumps(report,indent=2,ensure_ascii=False)+'\n')
        temporary.replace(destination)

    def api(method, params=None):
        request = Request(options.url+'/api/call', data=json.dumps({'method':method,'params':params or {}}).encode(), headers={'Content-Type':'application/json','Origin':options.url})
        with urlopen(request,timeout=40) as response:
            return json.load(response)

    def wait_job(job_id):
        deadline=time.monotonic()+900
        while time.monotonic()<deadline:
            job=api('video.job.get',{'workspaceId':workspace_id,'id':job_id})
            if job['status'] in ('completed','failed','cancelled','interrupted'):
                report['jobs'].append(job);save();return job
            time.sleep(1)
        api('video.job.cancel',{'workspaceId':workspace_id,'id':job_id})
        raise TimeoutError('Video job exceeded the qualification deadline')

    try:
        state=api('state');report['runtimeBefore']=state.get('runtime');report['models']=state.get('models')
        added=api('workspace.add',{'name':'Visual video qualification','path':str(workspace)})
        workspace_id=added['id'];report['workspaceId']=workspace_id
        imported=api('video.import',{'workspaceId':workspace_id,'path':manifest['clip']})
        clip_id=imported['clipId'];report['clipId']=clip_id
        if wait_job(imported['jobId'])['status']!='completed':raise RuntimeError('Import failed')
        detail=api('video.get',{'workspaceId':workspace_id,'clipId':clip_id});report['import']=detail
        media=api('video.media.open',{'workspaceId':workspace_id,'clipId':clip_id,'kind':'original'})
        assert media['url'].startswith('/api/media/')
        request=Request(options.url+media['url'],headers={'Range':'bytes=0-31','Origin':options.url})
        with urlopen(request,timeout=20) as response:
            body=response.read();report['range']={'status':response.status,'contentRange':response.headers.get('Content-Range'),'contentType':response.headers.get('Content-Type'),'bytes':len(body),'matchesSourcePrefix':body==source.read_bytes()[:32]}
        indexed=api('video.index',{'workspaceId':workspace_id,'clipId':clip_id})
        if wait_job(indexed['jobId'])['status']!='completed':raise RuntimeError('Indexing failed')
        report['indexed']=api('video.get',{'workspaceId':workspace_id,'clipId':clip_id});save()
        questions=['What shape is the red object?','What shape is the blue object?','What shape is the green object?','What shape is the yellow object?','How many purple objects are visible, and what shape are they?','What shape is the orange object?','What color is the dog in the clip?']
        for number,question in enumerate(questions):
            pending=api('video.ask',{'workspaceId':workspace_id,'clipId':clip_id,'question':question,'model':options.model})
            job=wait_job(pending['jobId'])
            answer=api('video.answer.get',{'workspaceId':workspace_id,'id':pending['answerId']})
            entry={'question':question,'jobStatus':job['status'],'answer':answer}
            if number<6:
                event=manifest['events'][number]
                entry['retrievalHit']=any(event['fromSeconds']<=e['timeSeconds']<event['toSeconds'] for e in answer.get('evidence',[]))
                entry['expectedInterval']=[event['fromSeconds'],event['toSeconds']]
            else:
                entry['negativeControl']=True
            report['answers'].append(entry);save()
        report['retrievalHits']=sum(e.get('retrievalHit',False) for e in report['answers'])
        report['sourceUnchanged']=sha(source)==manifest['clipSHA256']
        report['harnessUnchanged']=sha(Path(__file__))==harness_hash
        report['transportAndRetrievalPassed']=(report['range']['status']==206 and report['range']['matchesSourcePrefix'] and report['retrievalHits']>=5 and report['sourceUnchanged'] and report['harnessUnchanged'] and all(e['jobStatus']=='completed' for e in report['answers']))
        report['answerReview']='Pending independent comparison with the synthetic scene manifest; citation validation is not a semantic correctness judge.'
    except Exception as error:
        report['error']=str(error);report['transportAndRetrievalPassed']=False
        raise
    finally:
        report['durationSeconds']=round(time.monotonic()-started,3);report['finishedAt']=datetime.now(timezone.utc).isoformat();save()
    print(json.dumps({'report':str(destination),'retrievalHits':report['retrievalHits'],'transportAndRetrievalPassed':report['transportAndRetrievalPassed'],'durationSeconds':report['durationSeconds']}))
    if not report['transportAndRetrievalPassed']:
        raise RuntimeError('Video workflow qualification failed; report preserved')


if __name__=='__main__':
    main()
