"""Upgrade the three pilot fixtures to actual multi-process service projects."""
from pathlib import Path
import json
ROOT=Path(__file__).resolve().parents[2]/'internal/harness/fixtures'
def load(name):return json.loads((ROOT/name/'task.json').read_text())
def save(name,task,reference):
 (ROOT/name/'task.json').write_text(json.dumps(task,indent=2)+'\n')
 (ROOT/name/'reference.json').write_text(json.dumps(reference,indent=2)+'\n')

def python_project():
 t=load('python-ledger');p=t['public'];p.pop('logic.py')
 p['database.py']='''import sqlite3
def connect(path):
 db=sqlite3.connect(path)
 db.execute("CREATE TABLE IF NOT EXISTS events(id TEXT PRIMARY KEY, account TEXT NOT NULL, amount INTEGER NOT NULL, status TEXT NOT NULL)")
 return db
'''
 p['repository.py']='''from database import connect
class Ledger:
 def __init__(self,path): self.db=connect(path)
 def record(self,events):
  # Implement atomic first-write-wins ingestion of posted AND pending events.
  raise NotImplementedError("persistent event ingestion")
 def totals(self):
  # Return only posted account totals, including refunds and zero totals.
  return {}
 def close(self): self.db.close()
'''
 p['service.py']='''from repository import Ledger
def apply(path,events):
 ledger=Ledger(path)
 try:
  ledger.record(events)
  return ledger.totals()
 finally: ledger.close()
'''
 p['main.py']='''import json,sys,tempfile,os
from service import apply
def main():
 args=sys.argv[1:]; database=None
 if args[:1]==["--db"]: database=args[1];args=args[2:]
 inputs=json.loads(args[0]);out=[]
 for x in inputs:
  if database: out.append(apply(database,x["events"]))
  else:
   with tempfile.TemporaryDirectory() as tmp:out.append(apply(os.path.join(tmp,"ledger.db"),x["events"]))
 print(json.dumps(out))
if __name__=="__main__":main()
'''
 p['public_test.py']+='''
import os,tempfile
with tempfile.TemporaryDirectory() as tmp:
 db=os.path.join(tmp,"ledger.db")
 def run(events):
  return json.loads(subprocess.check_output([sys.executable,"main.py","--db",db,json.dumps([{ "events":events }])],text=True))[0]
 assert run([{"id":"a","account":"store","amount":100,"status":"posted"}])=={"store":100}
 assert run([])=={"store":100},"a new process must retain committed state"
print("cross-process persistence passed")
'''
 p['README.md']+='''
## Feature work: durable ledger

Implement `Ledger.record` and `Ledger.totals` in the supplied SQLite repository. Ingest the complete batch atomically; `id` is a permanent first-write-wins key, including pending events. `main.py --db PATH JSON` must persist across separate processes, and an empty events batch reads current totals. Keep the database, repository, service and CLI modules. Preserve original files `README.md`, `main.py`, `database.py`, `service.py`, and `public_test.py`; add regression tests in a new file and have the existing test command execute them (the runner discovers `test_*.py` below). Do not add dependencies.
'''
 p['public_test.py']+='''
import unittest
suite=unittest.defaultTestLoader.discover(".",pattern="test_*.py")
if not unittest.TextTestRunner().run(suite).wasSuccessful():raise SystemExit(1)
'''
 reference={'repository.py':'''from database import connect
class Ledger:
 def __init__(self,path):self.db=connect(path)
 def record(self,events):
  with self.db:
   self.db.executemany("INSERT OR IGNORE INTO events(id,account,amount,status) VALUES(?,?,?,?)",[(e["id"],e["account"],e["amount"],e["status"]) for e in events])
 def totals(self):return {account:amount for account,amount in self.db.execute("SELECT account,SUM(amount) FROM events WHERE status='posted' GROUP BY account")}
 def close(self):self.db.close()
''','test_regression.py':'''import unittest,tempfile,os
from service import apply
class Regression(unittest.TestCase):
 def test_pending_id_is_permanent(self):
  with tempfile.TemporaryDirectory() as d:
   p=os.path.join(d,"db");apply(p,[{"id":"a","account":"x","amount":4,"status":"pending"}]);self.assertEqual(apply(p,[{"id":"a","account":"x","amount":9,"status":"posted"}]),{})
'''}
 probe='''import json,subprocess,sys,tempfile,os,sqlite3
with tempfile.TemporaryDirectory() as tmp:
 db=os.path.join(tmp,"persistent.db")
 def run(events):return json.loads(subprocess.check_output([sys.executable,"main.py","--db",db,json.dumps([{"events":events}])],text=True))[0]
 first=run([{"id":"p","account":"a","amount":900,"status":"pending"},{"id":"x","account":"a","amount":110,"status":"posted"}])
 second=run([{"id":"p","account":"a","amount":900,"status":"posted"},{"id":"x","account":"a","amount":999,"status":"posted"},{"id":"r","account":"a","amount":-25,"status":"posted"}])
 third=run([])
 with sqlite3.connect(db) as c:rows=c.execute("SELECT COUNT(*) FROM events").fetchone()[0];integrity=c.execute("PRAGMA integrity_check").fetchone()[0]
 print(json.dumps({"first":first,"second":second,"third":third,"rows":rows,"integrity":integrity}))
'''
 t['probe']={'name':'independent SQLite reopen, deduplication, refund and integrity','command':['python3','_harness_probe.py'],'files':{'_harness_probe.py':probe},'expected':{'first':{'a':110},'second':{'a':85},'third':{'a':85},'rows':3,'integrity':'ok'}}
 t['protectedFiles']=['README.md','main.py','database.py','service.py','public_test.py'];t['name']='Persistent payment ledger';save(t['id'],t,reference)

def node_project():
 t=load('node-inventory');p=t['public'];p.pop('logic.cjs');p.pop('main.cjs');p['package.json']=json.dumps({'private':True,'scripts':{'test':'node public_test.cjs'}})
 p['src/stock.ts']='''export function available(input:any):Record<string,number> {
 // Implement expiry-aware reservations; preserve zero-stock SKUs and clamp at zero.
 throw new Error("implement inventory availability");
}
'''
 p['src/worker.ts']='''declare const require:any;
import {available} from './stock';
const {parentPort}=require('node:worker_threads');
parentPort.on('message',(m:any)=>setTimeout(()=>{
 try{parentPort.postMessage({id:m.id,value:available(m.input)});}catch(error:any){parentPort.postMessage({id:m.id,error:String(error)});}
},(m.id%3)*3));
'''
 p['src/main.ts']='''declare const require:any,process:any,__dirname:string;
export {};
const {Worker}=require('node:worker_threads');
async function main(){
 const inputs=JSON.parse(process.argv[2]);const results:any[]=[];
 const worker=new Worker(__dirname+'/worker.js');
 try{
  await new Promise<void>((resolve,reject)=>{
   if(!inputs.length){resolve();return;}
   worker.on('error',reject);
   worker.on('message',(m:any)=>{
    if(m.error){reject(new Error(m.error));return;}
    results.push(m.value); // Repair: worker completion order differs from request order.
    if(results.length===inputs.length)resolve();
   });
   inputs.forEach((input:any,id:number)=>worker.postMessage({id,input}));
  });
  console.log(JSON.stringify(results));
 } finally {await worker.terminate();}
}
main().catch(e=>{console.error(String(e));process.exitCode=1;});
'''
 p['run.cjs']='''const fs=require('node:fs'),os=require('node:os'),path=require('node:path'),cp=require('node:child_process');
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'inventory-ts-'));
try{
 cp.execFileSync('tsc',['--target','ES2022','--module','commonjs','--outDir',dir,'src/main.ts','src/worker.ts','src/stock.ts'],{stdio:['ignore','pipe','pipe']});
 process.stdout.write(cp.execFileSync(process.execPath,[path.join(dir,'main.js'),process.argv[2]],{encoding:'utf8',timeout:10000}));
}finally{fs.rmSync(dir,{recursive:true,force:true});}
'''
 p['public_test.cjs']=p['public_test.cjs'].replace('main.cjs','run.cjs')
 p['README.md']=p['README.md'].replace('node public_test.cjs','node public_test.cjs')+'''
## Feature work: TypeScript worker service

Implement `available` in src/stock.ts and repair response ordering in src/main.ts. `run.cjs` compiles TypeScript with the installed offline tsc, launches the compiled service, and removes temporary output. Every input goes through the real worker thread; completion order may differ, but output order must match input order. Terminate the worker on success and error. Preserve README.md, run.cjs, src/worker.ts, public_test.cjs and package.json. Add a new *.test.cjs regression file; the prescribed command discovers these. No package installation is needed.
'''
 p['README.md']=p['README.md'].replace('The CLI accepts','`node run.cjs` is the CLI. The CLI accepts')
 reference={'src/stock.ts':'''export function available(input:any):Record<string,number>{const out={...input.stock};for(const r of input.reservations){if(Object.prototype.hasOwnProperty.call(out,r.sku)&&r.expires>input.now)out[r.sku]-=r.quantity;}for(const k of Object.keys(out))out[k]=Math.max(0,out[k]);return out;}
''','src/main.ts':p['src/main.ts'].replace('results.push(m.value); // Repair: worker completion order differs from request order.','results[m.id]=m.value; completed++;').replace('const worker=new Worker','let completed=0;const worker=new Worker').replace('if(results.length===inputs.length)','if(completed===inputs.length)'), 'ordering.test.cjs':'''const assert=require('node:assert/strict'),cp=require('node:child_process');const inputs=Array.from({length:6},(_,i)=>({stock:{a:i},reservations:[],now:1}));assert.deepEqual(JSON.parse(cp.execFileSync(process.execPath,['run.cjs',JSON.stringify(inputs)],{encoding:'utf8'})),inputs.map(x=>x.stock));
'''}
 t['gradeCommand']=['node','run.cjs']
 inputs=[{'stock':{'a':i,'b':2},'reservations':[{'sku':'a','quantity':1,'expires':10},{'sku':'b','quantity':8,'expires':11}],'now':10} for i in range(9)]
 probe="const cp=require('node:child_process');const inputs="+json.dumps(inputs)+";process.stdout.write(cp.execFileSync(process.execPath,['run.cjs',JSON.stringify(inputs)],{encoding:'utf8',timeout:20000}));\n"
 t['probe']={'name':'independent TypeScript build and out-of-order worker correlation','command':['node','_harness_probe.cjs'],'files':{'_harness_probe.cjs':probe},'expected':[{'a':i,'b':0} for i in range(9)]};t['protectedFiles']=['README.md','run.cjs','src/worker.ts','public_test.cjs','package.json'];t['name']='TypeScript inventory worker service';save(t['id'],t,reference)

def go_project():
 t=load('go-inventory');p=t['public'];reference=json.loads((ROOT/t['id']/'reference.json').read_text());p['server.go']='''package main
import("encoding/json";"net/http")
// NewHandler must expose POST /availability with the same object contract as solve.
// Other paths return404; other methods return405; malformed JSON returns400.
func NewHandler()http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
 _=json.NewEncoder;http.Error(w,"not implemented",http.StatusNotImplemented)
})}
'''
 p['server_test.go']='''package main
import("testing";"net/http/httptest";"net/http";"strings";"encoding/json")
func TestPublicHTTP(t *testing.T){server:=httptest.NewServer(NewHandler());defer server.Close();resp,e:=http.Post(server.URL+"/availability","application/json",strings.NewReader(`{"stock":{"a":8},"reservations":[],"now":10}`));if e!=nil{t.Fatal(e)};defer resp.Body.Close();var out map[string]int;_ =json.NewDecoder(resp.Body).Decode(&out);if resp.StatusCode!=200||out["a"]!=8{t.Fatalf("status %d result%v",resp.StatusCode,out)}}
'''
 p['README.md']+='''
## Feature work: local HTTP API

Implement NewHandler in server.go for POST /availability. It accepts one input object and returns the same result as solve, with application/json content type. Unknown paths return404, non-POST methods return405, malformed JSON and multiple trailing JSON values return400. Handle simultaneous requests without sharing mutable result maps. The supplied HTTP test uses an actual temporary server and client. Keep the CLI and original README.md, main.go, logic_test.go, server_test.go and go.mod. Add a new Go *_test.go regression file; `go test ./...` discovers it. Use only the standard library.
'''
 reference['server.go']='''package main
import("encoding/json";"net/http";"io")
func NewHandler()http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){if r.URL.Path!="/availability"{http.NotFound(w,r);return};if r.Method!="POST"{w.WriteHeader(405);return};var x map[string]any;dec:=json.NewDecoder(r.Body);if e:=dec.Decode(&x);e!=nil{http.Error(w,"bad JSON",400);return};var extra any;if dec.Decode(&extra)!=io.EOF{http.Error(w,"trailing JSON",400);return};w.Header().Set("Content-Type","application/json");_ =json.NewEncoder(w).Encode(solve(x))})}
'''
 reference['regression_test.go']='''package main
import("testing";"net/http/httptest";"net/http";"strings")
func TestRejectTrailingJSON(t *testing.T){s:=httptest.NewServer(NewHandler());defer s.Close();r,e:=http.Post(s.URL+"/availability","application/json",strings.NewReader(`{} {}`));if e!=nil{t.Fatal(e)};defer r.Body.Close();if r.StatusCode!=400{t.Fatal(r.StatusCode)}}
'''
 probe='''package main
import("testing";"net/http/httptest";"net/http";"strings";"encoding/json";"fmt")
func TestHarnessObserve(t *testing.T){s:=httptest.NewServer(NewHandler());defer s.Close();statuses:=[]int{};bodies:=[]any{};for _,v:=range []struct{method,path,body string}{{"GET","/availability",""},{"POST","/missing","{}"},{"POST","/availability","bad"},{"POST","/availability","{} {}"},{"POST","/availability",`{"stock":{"a":8,"b":2},"reservations":[{"sku":"a","quantity":4,"expires":10},{"sku":"b","quantity":9,"expires":11}],"now":10}`}}{req,_:=http.NewRequest(v.method,s.URL+v.path,strings.NewReader(v.body));resp,e:=http.DefaultClient.Do(req);if e!=nil{t.Fatal(e)};statuses=append(statuses,resp.StatusCode);if resp.StatusCode==200{var out any;_ =json.NewDecoder(resp.Body).Decode(&out);bodies=append(bodies,out)};resp.Body.Close()};b,_:=json.Marshal(map[string]any{"statuses":statuses,"bodies":bodies});fmt.Println("HARNESS_OBSERVATION:"+string(b))}
'''
 # The wrapper returns only the observation, while the expected result stays on host.
 wrapper='''import subprocess,json
p=subprocess.run(["go","test","-run","^TestHarnessObserve$","-v","."],text=True,capture_output=True)
if p.returncode:raise RuntimeError(p.stdout+p.stderr)
lines=[x.split("HARNESS_OBSERVATION:",1)[1] for x in p.stdout.splitlines() if x.startswith("HARNESS_OBSERVATION:")]
if len(lines)!=1:raise RuntimeError("missing observation")
print(lines[0])
'''
 t['probe']={'name':'independent Go HTTP process, routing and malformed-request observations','command':['python3','_harness_probe.py'],'files':{'harness_probe_test.go':probe,'_harness_probe.py':wrapper},'expected':{'statuses':[405,404,400,400,200],'bodies':[{'a':8,'b':0}]}};t['protectedFiles']=['README.md','main.go','logic_test.go','server_test.go','go.mod'];t['name']='Go inventory HTTP service';save(t['id'],t,reference)

if __name__=='__main__':
 python_project();node_project();go_project();print('Expanded Python SQLite, Node TypeScript worker and Go HTTP pilot projects')
