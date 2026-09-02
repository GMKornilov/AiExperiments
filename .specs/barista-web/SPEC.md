# Web-интерфейс AI-бариста

## Назначение

Web-приложение предоставляет браузерный интерфейс поверх той же логики `free|controlled`, которую использует CLI. API-ключ и system prompts остаются только на backend. Один submit пользователя создаёт ровно один запрос к LLM API.

## Архитектура

- Общий Go-сервис маршрутизирует `free` и `controlled` запросы и используется CLI и API backend.
- Тонкая Go-команда `backend/cmd/api-server` разбирает параметры, собирает зависимости и управляет жизненным циклом HTTP-сервера; приватный пакет `backend/internal/httpapi` владеет JSON API.
- Отдельное приложение `frontend/` использует актуальный стабильный Next.js, App Router, TypeScript strict и `src/` layout.
- `frontend/src/app/page.tsx` остаётся Server Component, а интерактивная рабочая поверхность изолирована в минимальном Client Component внутри `src/features/barista`.
- `frontend/src/app/api/chat/route.ts` является BFF Route Handler: браузер вызывает только same-origin `/api/chat`, а Route Handler проксирует запрос в приватный Go backend через server-only `BARISTA_BACKEND_URL`.
- Go API запускается с defaults `config.yaml` и `:8080`; Next.js — на `:3000`.

## HTTP-контракт

### Go backend: `POST /api/chat`

Request:

```json
{"mode":"controlled","prompt":"Как настроить эспрессо?"}
```

- `mode`: строго `free` или `controlled`.
- `prompt`: непустая после trim строка, не более 4000 UTF-8 символов.
- Тело запроса ограничено 64 KiB и не допускает неизвестных полей или второго JSON-значения.

Success:

```json
{
  "mode": "controlled",
  "raw": "{\"summary\":...}",
  "data": {"summary":"...","focus_points":[],"recipe":{}}
}
```

- `raw` содержит исходный текст `choices[0].message.content`, возвращённый общей LLM-логикой.
- В controlled-режиме `data` содержит разобранный JSON-объект, уже прошедший schema и word-limit validation.
- В free-режиме `data` равно `null`, а `raw` содержит свободный текст.
- Ошибка запроса возвращает HTTP 400, ошибка LLM/валидации ответа — HTTP 502, с телом `{"error":"..."}`.

### Go backend: `GET /healthz`

Возвращает HTTP 200 и `{"status":"ok"}` без обращения к LLM.

### Next.js BFF

- `POST /api/chat` принимает browser request, проверяет content type, размер и поля `mode`/`prompt`, затем проксирует нормализованный контракт в Go backend с timeout и `cache: 'no-store'`.
- `GET /api/health` проверяет доступность Go backend и используется frontend healthcheck.
- Приватный `BARISTA_BACKEND_URL` доступен только server-side и не включается в browser bundle.
- Внутренние proxy/network ошибки возвращаются как безопасный JSON без backend URL и stack trace.

## Интерфейс

- Первый экран сразу показывает выбор режима, поле запроса и submit.
- Есть состояния empty, loading, error и success; повторный submit во время loading заблокирован.
- В free-режиме основной результат отображается как Markdown.
- В controlled-режиме JSON визуализируется предметно: строковые поля `summary` и три `focus_points` рендерятся как Markdown, recipe metrics (`coffee_g`, `water_g`, `temperature_c`, `brew_time_sec`) остаются значениями.
- `summary` обращён к пользователю в прямой рекомендательной форме (например, «Начните с…», «Уменьшите…») либо через рекомендацию («Я рекомендую взять…»). Ассистент не представлен исполнителем кофейного действия.
- Markdown не исполняет raw HTML и не отображает внешние изображения. Под визуальным результатом находится закрытый по умолчанию нативный spoiler (`details`) с дословным plain-text raw response в `pre`.
- UI адаптивен для мобильного и desktop, управляется клавиатурой, использует доступные labels/focus states и не вставляет ответ через `innerHTML`.

## Docker

- `backend/Dockerfile` собирает `backend/cmd/llm-chat` и `backend/cmd/api-server`; Go runtime содержит prompts и schemas, но не `config.yaml` или API-ключ.
- `backend/.gitignore` исключает backend-конфигурацию и build/cache/coverage artifacts, а `frontend/.gitignore` — Next.js и frontend artifacts.
- `frontend/Dockerfile` использует Next.js `output: 'standalone'`, запускается непривилегированным пользователем и не содержит server env или dev dependencies.
- `docker compose` запускает приватный `barista-api` и публичный `barista-web:3000`; frontend обращается к backend по внутренней Docker network.
- README описывает локальный запуск двух процессов, Docker Compose и использование CLI из Go-образа.

## Acceptance criteria

- AC-WEB-01: Next.js `GET /` возвращает рабочую поверхность, а Go `GET /healthz` и Next.js `GET /api/health` возвращают успешный health status.
- AC-WEB-02: валидный free request вызывает общий сервис один раз и возвращает `raw` с `data:null`.
- AC-WEB-03: валидный controlled request вызывает общий сервис один раз и возвращает одинаковое содержание в `raw` и разобранном `data`.
- AC-WEB-04: пустой/длинный prompt, неизвестный mode/field, лишний JSON и oversized body отклоняются без LLM-вызова.
- AC-WEB-05: ошибки LLM и controlled validation не возвращают успешный payload.
- AC-WEB-06: frontend безопасно форматирует free-ответ и controlled-поля `summary`/`focus_points` как Markdown, не исполняет raw HTML и не загружает изображения; raw показывается дословно только после открытия spoiler.
- AC-WEB-07: CLI сохраняет поведение и использует тот же общий сервис режимов.
- AC-WEB-08: frontend lint, typecheck, tests и production build проходят; only интерактивная feature boundary является Client Component.
- AC-DOCKER-01: оба Docker image собираются, Compose healthchecks проходят с примонтированным config, секрет отсутствует в image layers/source files.
