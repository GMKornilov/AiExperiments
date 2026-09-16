# Frontend AI-бариста

Next.js App Router приложение с проектами: каждый проект содержит независимые
чаты, а панель памяти показывает read-only общие и проектные факты. Браузер
обращается только к same-origin BFF `/api/projects/*`; Route Handlers создают
HttpOnly `SameSite=Lax` cookie и передают её ID backend через `X-Session-ID`.
Приватный `BARISTA_BACKEND_URL` доступен лишь server-side коду.

```sh
cp .env.example .env.local
npm ci
npm run dev
npm run lint && npm run typecheck && npm test && npm run build
```

Browser-регрессия активной модели памяти описана в [e2e/README.md](e2e/README.md).

```sh
npm run test:e2e
```
