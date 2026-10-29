# Gemma Desktop shared build contracts

Module: `gemmadesktop.local/app`. Integration owns go.mod/go.sum; specialists send dependency requests. Toolchain env: GOPATH=$PWD/.cache/go, GOCACHE=$PWD/.cache/gobuild, GOTOOLCHAIN=go1.26.10. Shared directory: do not overwrite another lane's files.

Frontend calls `api(method, params)` through `frontend/src/bridge.ts`. The application returns JSON objects (camelCase). Wails bridge will expose `Service.Call(method string, payload string) (string,error)`; development browser uses POST /api/call {method,params}. A snapshot poll every 700ms while busy is acceptable initially; events expose cursor-based catch-up. Error responses must be shown; never simulate progress.

API methods:
- `state`: params {}; returns {workspaces,sessions,runs,agents,workflows,models,artifacts,approvals,tools,skills,settings,runtime,events,experiments}.
- `workspace.add`: {path,name?}; `workspace.pick`: native folder picker; `workspace.remove`: {id}.
- `session.create`: {workspaceId,title?}; `session.get`: {id}; returns {session,messages,events}; `session.delete`: {id}.
- `run.start`: {sessionId,prompt,agentId?,model?,attachments?:string[]}; `run.cancel`: {id}; `approval.respond`: {id,allow}.
- `run.graph`: {id}; returns {runId,source:'checkpoint'|'events',workflow?:{id,name,nodes:[{id,type,label,position}],edges:[{id,source,target,condition?}]},reason?}. Read-only bounded diagram metadata from the run's frozen checkpoint, never the current editable workflow. Prompts and ADK snapshots are omitted. Missing legacy definitions yield explicitly labelled event-only graphs. Runtime tool outcomes can link to starts via `data.callId` / start `data.id`; fan-out children identify their declared parent via `data.parentNodeId`. Missing identities must not be inferred from ordering or node-name prefixes.
- `file.list`: {workspaceId,path?}; `file.read`: {workspaceId,path}; `file.write`: {workspaceId,path,content,expectedHash}; `git.diff`: {workspaceId}.
- `model.refresh`: {}; `model.pull`: {name}; `model.cancelPull`: {name}; `model.load`/`model.unload`/`model.delete`: {name}.
- `agent.save`: {id?,name,instructions,model,maxSteps,mode}; `agent.delete`: {id}.
- `workflow.save`: {id?,name,description,nodes,edges}; `workflow.run`: {id,sessionId,prompt}; `workflow.delete`: {id}.
- `mcp.save`: {id?,name,transport,command?,args?:string[],url?,enabled}; `mcp.connect`: {id}; `mcp.delete`: {id}.
- `skill.import`: {path}; `skill.delete`: {id}; `artifact.open`/`artifact.reveal`: {id}; `diagnostics.export`: {}.
- `settings.save`: {ollamaUrl?,defaultModel?,contextSize?,maxSteps?,mode?,theme?}.
- `terminal.start`: {workspaceId}; `terminal.write`: {id,data}; `terminal.resize`: {id,cols,rows}; `terminal.read`: {id,offset}; `terminal.stop`: {id}.
- `eval.start`: {workspaceId,baselineAgentId,candidateAgentId,tasks?:string[]}; `eval.cancel`: {id}.

Shared UI entity shapes (IDs strings, timestamps ISO8601):
- workspace {id,name,path,createdAt}; session {id,workspaceId,title,createdAt,updatedAt}; message {id,sessionId,role,content,createdAt,runId?}.
- run {id,sessionId,workspaceId,status,model,prompt,startedAt,finishedAt?,error?,agentId?,workflowId?,summary?}; statuses queued/running/waiting_approval/cancelling/completed/failed/cancelled/interrupted.
- event {id,seq,runId,type,title,detail,createdAt,nodeId?,tool?,data?}; types text/thinking/tool_started/tool_completed/tool_failed/approval/node_started/node_completed/node_failed/run_completed/error/status.
- approval {id,runId,tool,arguments,reason,status,createdAt}; artifact {id,runId,workspaceId,name,path,mediaType,size,createdAt,before?,after?}.
- agent {id,name,instructions,model,maxSteps,mode}; workflow {id,name,description,nodes:[{id,type,label,agentId?,prompt?,condition?,position:{x,y}}],edges:[{id,source,target,label?,condition?}]}.
- model {name,size,digest,family,parameterSize,quantization,capabilities?:string[],loaded?,remote?,contextLength?,runner?}; runtime {available,version,error?,memoryBytes?,models?:any[],pulls?:any[]}.
- settings {ollamaUrl,defaultModel,contextSize,maxSteps,mode,theme}; defaults local-only, gemma4:12b-mlx, context32768, maxSteps24, mode approval, dark.
- tool/MCP {id,name,transport,command,args,url,enabled,status,error?,tools?:any[]}; skill {id,name,description,path}; experiment {id,status,createdAt,results?:any[],error?}.

All UI actions use backend state and errors. Empty lists stay empty. Never insert fictional conversations, models, activity or metrics. Disable controls with a specific reason while their actual service is unavailable. UI content uses plain sanitized Markdown; no raw HTML/iframes in privileged webview.
