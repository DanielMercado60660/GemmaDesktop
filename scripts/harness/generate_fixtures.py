"""Author the bounded local fixture suite. Never execute candidate code on host."""
from pathlib import Path
import json, hashlib

ROOT = Path(__file__).resolve().parents[2] / 'internal/harness/fixtures'
FAMILIES = [
 ('ledger', 'Reconcile payment events', 'Return an object of account totals in integer cents. Include only status posted. Deduplicate by event id, keeping its first occurrence even when pending. Signed amounts include refunds. Do not mutate input.',
  [{'events':[]},{'events':[{'id':'a','account':'shop','amount':100,'status':'posted'}]}, {'events':[{'id':'a','account':'shop','amount':100,'status':'posted'},{'id':'b','account':'shop','amount':-25,'status':'posted'},{'id':'c','account':'other','amount':70,'status':'pending'}]}, {'events':[{'id':'a','account':'x','amount':9,'status':'pending'},{'id':'a','account':'x','amount':50,'status':'posted'},{'id':'b','account':'y','amount':0,'status':'posted'}]}, {'events':[{'id':'x','account':'a','amount':3,'status':'posted'},{'id':'x','account':'a','amount':3,'status':'posted'},{'id':'z','account':'a','amount':-5,'status':'posted'}]}],
  '''seen=set(); out={}\nfor e in x["events"]:\n if e["id"] in seen: continue\n seen.add(e["id"])\n if e["status"]=="posted": out[e["account"]]=out.get(e["account"],0)+e["amount"]\nreturn out''',
  '''const seen=new Set(), out={}; for(const e of x.events){if(seen.has(e.id))continue;seen.add(e.id);if(e.status==='posted')out[e.account]=(out[e.account]||0)+e.amount;}return out;''',
  '''seen:=map[string]bool{};out:=map[string]float64{};for _,raw:=range x["events"].([]any){e:=raw.(map[string]any);id:=e["id"].(string);if seen[id]{continue};seen[id]=true;if e["status"]=="posted"{out[e["account"].(string)]+=e["amount"].(float64)}};return out''', 'e["status"]=="posted"', "e.status==='posted'", 'e["status"]=="posted"'),
 ('inventory','Calculate available inventory','Return an object containing every stock SKU. Available = max(0, stock minus active reservations). A reservation is active only when expires > now. Unknown SKUs do not create stock. Do not mutate input.',
  [{'stock':{},'reservations':[],'now':10},{'stock':{'a':8},'reservations':[],'now':10},{'stock':{'a':8,'b':2},'reservations':[{'sku':'a','quantity':3,'expires':11},{'sku':'b','quantity':9,'expires':20}],'now':10},{'stock':{'a':8},'reservations':[{'sku':'a','quantity':4,'expires':10},{'sku':'a','quantity':1,'expires':9},{'sku':'ghost','quantity':3,'expires':30}],'now':10},{'stock':{'a':5,'b':0},'reservations':[{'sku':'a','quantity':2,'expires':20},{'sku':'a','quantity':2,'expires':20}],'now':10}],
  '''out=dict(x["stock"])\nfor r in x["reservations"]:\n if r["sku"] in out and r["expires"]>x["now"]: out[r["sku"]]-=r["quantity"]\nreturn {k:max(0,v) for k,v in out.items()}''',
  '''const out={...x.stock};for(const r of x.reservations){if(Object.hasOwn(out,r.sku)&&r.expires>x.now)out[r.sku]-=r.quantity;}for(const k of Object.keys(out))out[k]=Math.max(0,out[k]);return out;''',
  '''out:=map[string]float64{};for k,v:=range x["stock"].(map[string]any){out[k]=v.(float64)};for _,raw:=range x["reservations"].([]any){r:=raw.(map[string]any);k:=r["sku"].(string);if _,ok:=out[k];ok&&r["expires"].(float64)>x["now"].(float64){out[k]-=r["quantity"].(float64)}};for k,v:=range out{if v<0{out[k]=0}};return out''', 'r["expires"]>x["now"]', 'r.expires>x.now','r["expires"].(float64)>x["now"].(float64)'),
 ('orders','Resolve order update stream','Return the active order ids sorted lexicographically. Each id uses the greatest updatedAt timestamp; ties choose the later input record. Only that final record decides whether status cancelled excludes the id. Do not mutate input.',
  [{'orders':[]},{'orders':[{'id':'a','updatedAt':1,'status':'open'}]},{'orders':[{'id':'b','updatedAt':3,'status':'cancelled'},{'id':'b','updatedAt':1,'status':'open'},{'id':'a','updatedAt':2,'status':'open'}]},{'orders':[{'id':'a','updatedAt':2,'status':'open'},{'id':'a','updatedAt':2,'status':'cancelled'}]},{'orders':[{'id':'z','updatedAt':1,'status':'open'},{'id':'a','updatedAt':3,'status':'cancelled'},{'id':'a','updatedAt':4,'status':'open'}]}],
  '''latest={}\nfor o in x["orders"]:\n if o["id"] not in latest or o["updatedAt"]>=latest[o["id"]]["updatedAt"]: latest[o["id"]]=o\nreturn sorted(k for k,v in latest.items() if v["status"]!="cancelled")''',
  '''const latest=new Map();for(const o of x.orders){if(!latest.has(o.id)||o.updatedAt>=latest.get(o.id).updatedAt)latest.set(o.id,o);}return [...latest].filter(([k,v])=>v.status!=='cancelled').map(([k])=>k).sort();''',
  '''latest:=map[string]map[string]any{};for _,raw:=range x["orders"].([]any){o:=raw.(map[string]any);k:=o["id"].(string);if old,ok:=latest[k];!ok||o["updatedAt"].(float64)>=old["updatedAt"].(float64){latest[k]=o}};out:=[]string{};for k,v:=range latest{if v["status"]!="cancelled"{out=append(out,k)}};sort.Strings(out);return out''','o["updatedAt"]>=latest[o["id"]]["updatedAt"]','o.updatedAt>=latest.get(o.id).updatedAt','o["updatedAt"].(float64)>=old["updatedAt"].(float64)'),
 ('usage','Aggregate bounded usage windows','Return per-user total units for events in the closed time interval [now-window, now]. Include zero-unit users when an event qualifies. Exclude future and older events. Units are nonnegative integers. Do not mutate input.',
  [{'events':[],'now':100,'window':10},{'events':[{'user':'a','at':95,'units':2}],'now':100,'window':10},{'events':[{'user':'a','at':90,'units':3},{'user':'a','at':100,'units':2},{'user':'b','at':101,'units':9}],'now':100,'window':10},{'events':[{'user':'a','at':89,'units':10},{'user':'z','at':90,'units':0}],'now':100,'window':10},{'events':[{'user':'a','at':0,'units':1},{'user':'a','at':1,'units':2}],'now':0,'window':0}],
  '''out={}\nfor e in x["events"]:\n if x["now"]-x["window"]<=e["at"]<=x["now"]: out[e["user"]]=out.get(e["user"],0)+e["units"]\nreturn out''',
  '''const out={};for(const e of x.events){if(e.at>=x.now-x.window&&e.at<=x.now)out[e.user]=(out[e.user]||0)+e.units;}return out;''',
  '''out:=map[string]float64{};for _,raw:=range x["events"].([]any){e:=raw.(map[string]any);at:=e["at"].(float64);if at>=x["now"].(float64)-x["window"].(float64)&&at<=x["now"].(float64){out[e["user"].(string)]+=e["units"].(float64)}};return out''','<=e["at"]<=','<INVALID>','at>=x["now"].(float64)-x["window"].(float64)'),
 ('intervals','Normalize maintenance windows','Return sorted merged half-open [start,end] windows. Discard empty or reversed windows, merge overlapping AND touching windows, retain negative endpoints, and do not mutate input.',
  [{'windows':[]},{'windows':[[2,4]]},{'windows':[[5,8],[1,3],[3,5],[9,10]]},{'windows':[[3,3],[7,2],[-4,-1],[-2,1]]},{'windows':[[1,8],[2,4],[1,8],[10,11]]}],
  '''out=[]\nfor a,b in sorted(v for v in x["windows"] if v[0]<v[1]):\n if out and a<=out[-1][1]: out[-1][1]=max(out[-1][1],b)\n else: out.append([a,b])\nreturn out''',
  '''const out=[];const rows=x.windows.filter(([a,b])=>a<b).map(r=>[...r]).sort((a,b)=>a[0]-b[0]||a[1]-b[1]);for(const [a,b] of rows){if(out.length&&a<=out[out.length-1][1])out[out.length-1][1]=Math.max(out[out.length-1][1],b);else out.push([a,b]);}return out;''',
  '''rows:=[][2]float64{};for _,raw:=range x["windows"].([]any){v:=raw.([]any);a,b:=v[0].(float64),v[1].(float64);if a<b{rows=append(rows,[2]float64{a,b})}};sort.Slice(rows,func(i,j int)bool{return rows[i][0]<rows[j][0]||(rows[i][0]==rows[j][0]&&rows[i][1]<rows[j][1])});out:=[][2]float64{};for _,v:=range rows{n:=len(out);if n>0&&v[0]<=out[n-1][1]{if v[1]>out[n-1][1]{out[n-1][1]=v[1]}}else{out=append(out,v)}};return out''','a<=out[-1][1]','a<=out[out.length-1][1]','v[0]<=out[n-1][1]'),
 ('config','Apply layered service configuration','Merge layers from left to right recursively for objects. Null deletes a key. Arrays and other values replace earlier values; false, zero and empty strings remain valid. Return a new object without mutating any layer.',
  [{'layers':[]},{'layers':[{'a':1}]},{'layers':[{'service':{'port':80,'tls':True},'tags':['old']},{'service':{'port':0},'tags':[]}]},{'layers':[{'a':1,'b':2},{'a':None,'b':False,'c':''}]},{'layers':[{'a':{'x':1,'y':2}},{'a':{'x':None,'z':3}},{'a':{'y':0}}]}],
  '''def merge(a,b):\n out=dict(a)\n for k,v in b.items():\n  if v is None: out.pop(k,None)\n  elif isinstance(v,dict): out[k]=merge(out.get(k,{}) if isinstance(out.get(k),dict) else {},v)\n  else: out[k]=v\n return out\nout={}\nfor layer in x["layers"]: out=merge(out,layer)\nreturn out''',
  '''function merge(a,b){const out={...a};for(const [k,v] of Object.entries(b)){if(v===null)delete out[k];else if(typeof v==='object'&&!Array.isArray(v))out[k]=merge(out[k]&&typeof out[k]==='object'&&!Array.isArray(out[k])?out[k]:{},v);else out[k]=v;}return out;}return x.layers.reduce(merge,{});''',
  '''var merge func(map[string]any,map[string]any)map[string]any;merge=func(a,b map[string]any)map[string]any{out:=map[string]any{};for k,v:=range a{out[k]=v};for k,v:=range b{if v==nil{delete(out,k)}else if obj,ok:=v.(map[string]any);ok{old,_:=out[k].(map[string]any);out[k]=merge(old,obj)}else{out[k]=v}};return out};out:=map[string]any{};for _,v:=range x["layers"].([]any){out=merge(out,v.(map[string]any))};return out''','if v is None:','if(v===null)','if v==nil'),
]

def wrapped_py(body): return 'def solve(x):\n'+''.join(' '+line+'\n' for line in body.splitlines())
def oracle(body,value):
 scope={};exec(wrapped_py(body),scope);return scope['solve'](json.loads(json.dumps(value)))
def digest(v):return hashlib.sha256(json.dumps(v,sort_keys=True,separators=(',',':')).encode()).hexdigest()

def generate():
 manifest=[]
 for stack in ('python','node','go'):
  for i,(family,title,description,cases,py,js,go,pybug,jsbug,gobug) in enumerate(FAMILIES):
   ident=f'{stack}-{family}';split=('development','validation','heldout')[i//2]
   outputs=[oracle(py,c) for c in cases]
   if stack=='python':
    fixed=wrapped_py(py);broken=fixed.replace(pybug, 'False' if family not in ('usage','config') else ('<e["at"]<' if family=='usage' else 'if False:'),1)
    public={'logic.py':broken,'main.py':'import json,sys\nfrom logic import solve\nprint(json.dumps([solve(x) for x in json.loads(sys.argv[1])]))\n'};reference={'logic.py':fixed};command=['python3','public_test.py'];grade=['python3','main.py']
    public['public_test.py']='import json,subprocess,sys\ncases='+repr(cases[:2])+'\nexpected='+repr(outputs[:2])+'\np=subprocess.run([sys.executable,"main.py",json.dumps(cases)],capture_output=True,text=True,check=True)\nassert json.loads(p.stdout)==expected,(p.stdout,expected)\nprint("2 public cases passed")\n'
   elif stack=='node':
    fixed='exports.solve=function(x){'+js+'};\n';replacement='false' if family!='config' else 'if(false)';broken=fixed.replace(jsbug,replacement,1) if family!='usage' else fixed.replace('e.at>=x.now-x.window&&e.at<=x.now','e.at>x.now-x.window&&e.at<x.now')
    public={'logic.cjs':broken,'main.cjs':'const {solve}=require("./logic.cjs");console.log(JSON.stringify(JSON.parse(process.argv[2]).map(solve)));\n','package.json':json.dumps({'private':True,'scripts':{'test':'node public_test.cjs'}})};reference={'logic.cjs':fixed};command=['node','public_test.cjs'];grade=['node','main.cjs']
    public['public_test.cjs']='const assert=require("node:assert/strict"),{execFileSync}=require("node:child_process");const cases='+json.dumps(cases[:2])+',expected='+json.dumps(outputs[:2])+';assert.deepEqual(JSON.parse(execFileSync(process.execPath,["main.cjs",JSON.stringify(cases)],{encoding:"utf8"})),expected);console.log("2 public cases passed");\n'
   else:
    fixed='package main\nimport "sort"\nvar _ = sort.Strings\nfunc solve(x map[string]any) any {'+go+'}\n';replacement='false' if family!='config' else 'if false';broken=fixed.replace(gobug,replacement,1)
    public={'logic.go':broken,'main.go':'package main\nimport("encoding/json";"os";"fmt")\nfunc main(){var inputs []map[string]any;if e:=json.Unmarshal([]byte(os.Args[1]),&inputs);e!=nil{panic(e)};out:=[]any{};for _,x:=range inputs{out=append(out,solve(x))};b,e:=json.Marshal(out);if e!=nil{panic(e)};fmt.Println(string(b))}\n','go.mod':'module fixture.local/'+ident+'\n\ngo 1.23\n'};reference={'logic.go':fixed};command=['go','test','./...'];grade=['go','run','.']
    public['logic_test.go']='package main\nimport("testing";"encoding/json";"reflect")\nfunc TestPublic(t *testing.T){var inputs []map[string]any;_ = json.Unmarshal([]byte(`'+json.dumps(cases[:2])+'`),&inputs);var expected []any;_ = json.Unmarshal([]byte(`'+json.dumps(outputs[:2])+'`),&expected);for i,x:=range inputs{b,_:=json.Marshal(solve(x));var actual any;_ = json.Unmarshal(b,&actual);if !reflect.DeepEqual(actual,expected[i]){t.Fatalf("case %d: got %v want %v",i,actual,expected[i])}}}\n'
   if stack=='python':
    public['public_test.py']+='\nimport unittest\nsuite=unittest.defaultTestLoader.discover(".",pattern="test_*.py")\nif not unittest.TextTestRunner().run(suite).wasSuccessful():raise SystemExit(1)\n'
    reference['test_regression.py']='import unittest\nfrom logic import solve\nclass Regression(unittest.TestCase):\n def test_readme_example(self):self.assertEqual(solve('+repr(cases[2])+'),'+repr(outputs[2])+')\n'
   elif stack=='node':
    public['public_test.cjs']+='\nconst fs=require("node:fs");for(const file of fs.readdirSync(".").filter(x=>x.endsWith(".test.cjs")))require("./"+file);\n'
    reference['regression.test.cjs']='const assert=require("node:assert/strict"),{solve}=require("./logic.cjs");assert.deepEqual(solve('+json.dumps(cases[2])+'),'+json.dumps(outputs[2])+');\n'
   else:
    reference['regression_test.go']='package main\nimport("testing";"encoding/json";"reflect")\nfunc TestREADMERegression(t *testing.T){var x map[string]any;_ =json.Unmarshal([]byte(`'+json.dumps(cases[2])+'`),&x);b,_:=json.Marshal(solve(x));var got,want any;_ =json.Unmarshal(b,&got);_ =json.Unmarshal([]byte(`'+json.dumps(outputs[2])+'`),&want);if !reflect.DeepEqual(got,want){t.Fatal(got,want)}}\n'
   public['README.md']=f'# {title}\n\nRepair this {stack} service module and add regression tests.\n\n{description}\n\nThe CLI accepts one JSON array argument and prints one JSON array of corresponding results, with no extra stdout. Preserve this interface and the separate logic module. Run `{ " ".join(command) }` and report actual results. No external dependencies are needed.\n\nExample input:\n```json\n{json.dumps(cases[2],indent=2)}\n```\nExpected result:\n```json\n{json.dumps(outputs[2],indent=2)}\n```\n'
   task={'id':ident,'name':title,'stack':stack,'split':split,'prompt':f'Read README.md, repair {title.lower()}, add meaningful regression tests, and run the prescribed public command. Keep the public CLI compatible.','command':command,'gradeCommand':grade,'public':public,'cases':[{'name':f'{family}-{n+1}','input':c,'expected':o,'critical':False} for n,(c,o) in enumerate(zip(cases,outputs))]}
   directory=ROOT/ident;directory.mkdir(parents=True,exist_ok=True)
   (directory/'task.json').write_text(json.dumps(task,indent=2)+'\n')
   (directory/'reference.json').write_text(json.dumps(reference,indent=2)+'\n')
   manifest.append({'id':ident,'stack':stack,'split':split,'publicFiles':len(public),'cases':len(cases)})
 (ROOT/'manifest.json').write_text(json.dumps({'version':1,'tasks':manifest,'scope':'Local service fixtures, not Google benchmark replication. Each task has a separate CLI and logic module; public tests and independent behavior cases.'},indent=2)+'\n')
 print(f'Wrote {len(manifest)} fixtures')

if __name__=='__main__':generate()
