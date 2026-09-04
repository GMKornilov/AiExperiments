# AI-бариста frontend

Отдельное Next.js-приложение с App Router и TypeScript. Браузер вызывает same-origin Route Handlers `/api/chat`, `/api/algorithms/*` и `/api/temperature`; приватный Go backend задаётся только server-side переменной `BARISTA_BACKEND_URL` (для алгоритмов также поддерживается `ALGORITHMS_BACKEND_URL`).

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

Страница `/temperature` (`Неделя 1, задание 4`) содержит один prompt и управление
температурой: быстрые значения `0`, `0.7`, `1.2` либо собственное число от `0`
до `2`. Она отправляет один JSON-запрос `POST /api/temperature` вида
`{"prompt":"...","temperature":0.7}`. BFF нормализует prompt, проверяет
строго два поля, лимит 64 KiB и диапазон температуры, затем вызывает приватный
Go API по тому же пути. Черновик и результат не сохраняются между перезагрузками;
ответы и ошибки не кэшируются.
