# Frontend AI-бариста

Next.js App Router приложение. Браузер вызывает только same-origin `/api/*`.
Route Handlers выдают HttpOnly `SameSite=Lax` cookie и передают его значение
backend только в `X-Session-ID`; приватный `BARISTA_BACKEND_URL` доступен лишь
серверному коду.

```sh
cp .env.example .env.local
npm ci
npm run dev
npm run lint && npm run typecheck && npm test && npm run build
```
