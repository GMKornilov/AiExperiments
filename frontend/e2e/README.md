# Browser regression tests

Tests use an already running isolated test stack and do not start or stop
services. The repository fixture returns one 503 for `fail*` prompts, delays
`slow*` prompts by eight seconds and echoes all other prompts.

In separate terminals, from the repository root:

```sh
node frontend/e2e/fixtures/provider.mjs
(cd backend && go run ./cmd/api-server --config ../frontend/e2e/fixtures/backend.yaml)
(cd frontend && BARISTA_BACKEND_URL=http://127.0.0.1:18080 npm run dev -- --port 13000)
```

`backend.yaml`, nested `llm.yaml` и оба prompt в `fixtures/` содержат e2e-only
dummy credential. Set `BARISTA_E2E_URL` when using another frontend URL.

```sh
npm run test:e2e
```

Token regression and the simulated short/long/context-overflow comparison:

```sh
npm run test:e2e -- e2e/token-usage.spec.mjs
```

The fixture returns synthetic `usage` (100/20 for the first main call, 150/30
for the second), while title calls report 10000 tokens that must be excluded.
`tokens-growth-*` prompts are rejected after eight completed pairs with
`context_length_exceeded`. These are controlled fixture values, not DeepSeek
measurements. See [demo instructions](../../docs/token-usage-demo.md).

Use an isolated `history_path` when restarting the backend for QA. When Next.js
runs in Docker and the backend runs on the host, set `BARISTA_BACKEND_URL` to
`http://host.docker.internal:18080` and bind the test backend to `0.0.0.0:18080`.
