package execution

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/workspace"
)

// This acceptance fixture uses only embedded, independent test programs and
// installed tools. It does not read Harness candidates, references or graders.
func TestDockerLiveNPMBuildWorkerAndPythonSpawnSQLite(t *testing.T) {
	image := os.Getenv("GEMMA_TEST_DOCKER_IMAGE")
	if image == "" {
		t.Skip("opt-in: installed immutable GEMMA_TEST_DOCKER_IMAGE; no downloads")
	}
	dir, err := os.MkdirTemp("/tmp", "gemma-workflow-proof-")
	if err != nil {
		t.Fatal(err)
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "project")
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	d, err := NewDockerExecutor(DockerConfig{StateDir: filepath.Join(dir, "execution"), Image: image})
	if err != nil {
		_ = os.RemoveAll(dir)
		t.Fatal(err)
	}
	// Run is synchronous. Recovery therefore follows its complete unwind. If
	// Docker cleanup fails, preserve ownership records instead of deleting mounts.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := d.Reconcile(ctx); err != nil {
			t.Errorf("cleanup recovery failed; retained %s: %v", dir, err)
			return
		}
		if _, err := d.ReleaseWorkspaceCache(ctx, root); err != nil {
			t.Errorf("cache cleanup failed; retained %s: %v", dir, err)
			return
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	files := map[string]string{
		"package.json": `{"private":true,"scripts":{"build":"node build.cjs"}}`,
		"src/math.ts":  `export function double(value:number):number{return value*2;}`,
		"src/worker.ts": `declare const require:any;
import {double} from './math';
const {parentPort,threadId}=require('node:worker_threads');
parentPort.once('message',(value:number)=>parentPort.postMessage({value:double(value),threadId}));`,
		"build.cjs": `const assert=require('node:assert/strict');
const fs=require('node:fs'),os=require('node:os'),path=require('node:path');
const {execFileSync}=require('node:child_process');
const {Worker}=require('node:worker_threads');
const http=require('node:http');
async function main(){
 assert.equal(process.env.npm_lifecycle_event,'build');
 const output=fs.mkdtempSync(path.join(os.tmpdir(),'npm-build-proof-'));
 let worker,server;
 const watchdog=setTimeout(()=>{console.error('proof timed out');process.exit(1)},45000);
 try{
  execFileSync('tsc',['--target','ES2022','--module','commonjs','--outDir',output,'src/math.ts','src/worker.ts'],{stdio:['ignore','pipe','pipe'],timeout:30000});
  assert(fs.existsSync(path.join(output,'math.js')));
  assert(fs.existsSync(path.join(output,'worker.js')));
  worker=new Worker(path.join(output,'worker.js'));
  const message=new Promise((resolve,reject)=>{worker.once('message',resolve);worker.once('error',reject)});
  const exited=new Promise((resolve,reject)=>worker.once('exit',code=>code===0?resolve():reject(new Error('worker exit '+code))));
  worker.postMessage(21);
  const [result]=await Promise.all([message,exited]);
  assert.equal(result.value,42);assert(result.threadId>0);
  server=http.createServer((req,res)=>{res.setHeader('content-type','application/json');res.end(JSON.stringify(result))});
  await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(0,'127.0.0.1',resolve)});
  const response=await new Promise((resolve,reject)=>http.get({hostname:'127.0.0.1',port:server.address().port,path:'/'},res=>{
   let body='';res.setEncoding('utf8');res.on('data',chunk=>body+=chunk);res.on('end',()=>resolve({status:res.statusCode,body:JSON.parse(body)}));
  }).on('error',reject));
  assert.equal(response.status,200);assert.equal(response.body.value,42);
  console.log('EXECUTOR_NPM_PROOF '+JSON.stringify({npmLifecycle:'build',compiledModules:2,workerResult:result.value,workerThreadId:result.threadId,loopbackStatus:response.status,loopbackResult:response.body.value}));
 }finally{
  if(server)await new Promise(resolve=>server.close(resolve));
  if(worker)await worker.terminate();
  fs.rmSync(output,{recursive:true,force:true});clearTimeout(watchdog);
 }
}
main().catch(error=>{console.error(error);process.exitCode=1});`,
		"multiprocess_sqlite.py": `import json,multiprocessing,os,pathlib,sqlite3,subprocess,sys,tempfile
def write_row(database,index,queue):
    with sqlite3.connect(database,timeout=10) as db:
        db.execute('INSERT INTO records(id,label) VALUES (?,?)',(index,'worker-'+str(index)))
    db.close()
    queue.put({'id':index,'pid':os.getpid(),'method':multiprocessing.get_start_method()})
def main():
    context=multiprocessing.get_context('spawn')
    with tempfile.TemporaryDirectory(prefix='spawn-sqlite-proof-') as directory:
        database=str(pathlib.Path(directory)/'records.db')
        with sqlite3.connect(database) as db:
            assert db.execute('PRAGMA journal_mode=WAL').fetchone()[0]=='wal'
            db.execute('CREATE TABLE records(id INTEGER PRIMARY KEY,label TEXT NOT NULL)')
        db.close()
        queue=context.Queue()
        children=[context.Process(target=write_row,args=(database,index,queue)) for index in (1,2)]
        started=[]
        try:
            for child in children:
                child.start();started.append(child)
            receipts=[queue.get(timeout=15) for _ in children]
            for child in children:
                child.join(15)
                assert not child.is_alive() and child.exitcode==0,(child.pid,child.exitcode)
            assert sorted(r['id'] for r in receipts)==[1,2]
            assert all(r['method']=='spawn' and r['pid']!=os.getpid() for r in receipts)
            assert len({r['pid'] for r in receipts})==2
            reader="import json,sqlite3,sys; c=sqlite3.connect(sys.argv[1]); print(json.dumps({'rows':c.execute('select id,label from records order by id').fetchall(),'integrity':c.execute('pragma integrity_check').fetchone()[0]})); c.close()"
            reopened=json.loads(subprocess.check_output([sys.executable,'-c',reader,database],text=True,timeout=15))
            assert reopened=={'rows':[[1,'worker-1'],[2,'worker-2']],'integrity':'ok'},reopened
            print('EXECUTOR_PYTHON_PROOF '+json.dumps({'startMethod':'spawn','childCount':len(receipts),'distinctChildPIDs':len({r['pid'] for r in receipts}),'subprocessReopen':True,'rows':reopened['rows'],'integrity':reopened['integrity']}))
        finally:
            for child in started:
                if child.is_alive():child.terminate()
                child.join(5)
                if child.is_alive():child.kill();child.join(5)
            queue.close();queue.join_thread()
if __name__=='__main__':main()
`,
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, []byte(content), 0444); err != nil {
			t.Fatal(err)
		}
	}
	before, err := workspace.ScanCommandTree(t.Context(), root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, executable, marker string
		args                     []string
		want                     map[string]any
	}{
		{"npm-build-worker-http", "npm", "EXECUTOR_NPM_PROOF ", []string{"run", "build", "--offline", "--no-audit", "--no-fund", "--update-notifier=false"}, map[string]any{"npmLifecycle": "build", "compiledModules": float64(2), "workerResult": float64(42), "loopbackStatus": float64(200), "loopbackResult": float64(42)}},
		{"python-spawn-sqlite", "python3", "EXECUTOR_PYTHON_PROOF ", []string{"multiprocess_sqlite.py"}, map[string]any{"startMethod": "spawn", "childCount": float64(2), "distinctChildPIDs": float64(2), "subprocessReopen": true, "integrity": "ok"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			r, err := d.Run(ctx, Request{Workspace: root, Executable: tc.executable, Args: tc.args, ReadOnly: true, Timeout: 90 * time.Second, MaxOutput: 16 << 10})
			if err != nil || r.ExitCode != 0 || !r.ReadOnly || r.Publication != "read_only" || r.Cancelled || r.Truncated || r.Environment != "docker-linux" {
				t.Fatalf("real read-only workflow failed: %+v %v", r, err)
			}
			proof := map[string]any{}
			proofCount := 0
			for line := range strings.SplitSeq(r.Stdout, "\n") {
				if strings.HasPrefix(line, tc.marker) {
					proofCount++
					if err := json.Unmarshal([]byte(strings.TrimPrefix(line, tc.marker)), &proof); err != nil {
						t.Fatal(err)
					}
				}
			}
			if proofCount != 1 {
				t.Fatalf("expected one proof marker; got %d: %q", proofCount, r.Stdout)
			}
			for key, value := range tc.want {
				if proof[key] != value {
					t.Fatalf("proof %s: got %v, want %v", key, proof[key], value)
				}
			}
			after, err := workspace.ScanCommandTree(ctx, root, "", nil)
			if err != nil || !workspace.SameCommandManifest(before, after) {
				t.Fatalf("read-only source changed: %v", err)
			}
			ids, err := d.cli(ctx, 4096, "ps", "-aq", "--filter", "label=com.gemmadesktop.owner="+d.owner)
			if err != nil || strings.TrimSpace(ids) != "" {
				t.Fatalf("owned guest remains: %q %v", ids, err)
			}
			t.Logf("command=%s argv=%v receipt=%s image=%s publication=%s source=%s proof=%v", tc.executable, tc.args, r.CommandID, r.Image, r.Publication, before.Digest, proof)
		})
	}
}
