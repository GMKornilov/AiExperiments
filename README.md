# AI-бариста

Web-чат с несколькими диалогами и памятью в ОЗУ. Браузер обращается только к
same-origin Next.js BFF; ключ LLM, endpoint и system prompt остаются в backend.
После перезапуска backend диалоги и журнал стираются.

## Локальный запуск

```sh
cp backend/config.example.yaml backend/config.yaml
cp backend/llm.example.yaml backend/llm.yaml
# заполните api_key в backend/llm.yaml
cd backend && go run ./cmd/api-server --config=config.yaml
```

Во втором терминале выполните `cd frontend && cp .env.example .env.local && npm ci && npm run dev`.
Откройте [localhost:3000](http://localhost:3000). Журнал конкретного диалога доступен на `/admin`
по его точному ID.

## Docker Compose

```sh
docker compose up --build
```

Compose монтирует `backend/config.yaml`, вложенный `backend/llm.yaml`, chat и
title system prompts read-only. В `llm.yaml` обязательны секции `chat` для
основного ответа и `text` для фонового названия первого вопроса. Изменения LLM
YAML и prompt применяются к следующим созданным
диалогам; backend config применяется после перезапуска. Локальные конфиги
с ключами не включаются в образ и не должны попадать в Git.

## Поведение и проверки

У каждого диалога свой неизменяемый снимок LLM-настроек. После ошибки новое сообщение
заблокировано до ручного повтора; refresh восстанавливает принятую историю. Удаление
требует подтверждения и отменяет ожидающий ответ.

- [Спецификация](.specs/barista-agent/SPEC.md)
- [Диагностика и журнал](docs/llm-logs.md)
- [Browser-проверки](frontend/e2e/README.md)

```sh
cd backend && go test -race ./... && go vet ./...
```

```sh
cd frontend && npm run lint && npm run typecheck && npm test && npm run build
```
