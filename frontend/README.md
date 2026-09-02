# AI-бариста frontend

Отдельное Next.js-приложение с App Router и TypeScript. Браузер вызывает same-origin Route Handler `/api/chat`; приватный Go backend задаётся только server-side переменной `BARISTA_BACKEND_URL`.

```sh
cp .env.example .env.local
npm ci
npm run dev
```

Обязательные проверки:

```sh
npm run lint
npm run typecheck
npm test
npm run build
```
