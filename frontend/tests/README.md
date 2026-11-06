# Frontend checks

Run `npm ci`, `npm run typecheck`, `npm test`, and `npm run build` from `frontend`.
`npm test` runs the existing Node helper suite and the Vitest suite in `tests/ui`.
The TypeScript check includes rendered tests and fixtures.

The rendered suite uses React Testing Library, user-event, jsdom, and the real
confirmation/dialog components. App bridge calls are mocked; these tests do not
start inference, download models, install runtimes, or mutate a user's app data.
Workflow graph geometry is mocked; canvas layout and native keyboard/window
behavior still need the packaged app smoke test.

Coverage includes workflow revision admission, async editor changes and discard
focus, durable draft ownership/conflicts, readiness and effective profiles,
keyboard tabs, runtime installation/qualification controls, frozen reliability
schedules and bounded batches, review/promotion boundaries, incremental state,
message pagination, and independent opt-in harness controls. Helper-level
assertions cover persistence and pagination races that do not require a browser.

Run `npm run benchmark:history` separately for a rendered history workload. It
extracts the App, conversation, and composer modules from pinned commit
`d8dbce0741994373a5cc4677e62791d1ff59a806` into the ignored dependency cache,
then compares them with the current App using the same UI dependencies. Both
variants display the latest 40 messages in a 10,000-message conversation. Three
alternating samples each measure 12 unchanged polls after one warmup, including
JSON response encoding/decoding and React updates. The current path receives a
100-message initial page and unchanged metadata afterward.

The report is written to `/private/tmp/gemma-history-benchmark.json`. Assertions
check the visible history and poll count; timing is reported, not a flaky CI
threshold. This is a jsdom workload, not a native webview benchmark: it excludes
native IPC, backend queries, layout, paint, and GPU work. JSON response bytes are
synthetic bridge payload sizes, not a recorded Wails transport trace. Initial
mount times include warmup noise and should not be treated as startup evidence.
