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

`backend.yaml`, `llm.yaml` and the prompt in `fixtures/` contain an e2e-only
dummy credential. Set `BARISTA_E2E_URL` when using another frontend URL.

```sh
npm run test:e2e
```
