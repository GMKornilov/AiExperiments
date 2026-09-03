# AI-бариста frontend

Отдельное Next.js-приложение с App Router и TypeScript. Браузер вызывает same-origin Route Handlers `/api/chat` и `/api/algorithms/*`; приватный Go backend задаётся только server-side переменной `BARISTA_BACKEND_URL` (для алгоритмов также поддерживается `ALGORITHMS_BACKEND_URL`).

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

Для алгоритмов BFF использует единый deadline на запрос и чтение ответа backend:
185 секунд для обычных методов и 365 секунд для `generated-prompt`. Входящее тело
браузерского запроса по-прежнему ограничено 10 секундами.
