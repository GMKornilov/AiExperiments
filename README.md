# AI-бариста

Веб-агент для разговоров о кофе. Чаты объединены в проекты и изолированы
границей browser-сеанса. Браузер обращается к same-origin Next.js BFF; ключи и
приватный адрес LLM остаются в backend.

## Память агента

У агента три явно разделённых слоя:

- краткосрочная — последние `N` сообщений открытого чата;
- рабочая — факты текущего проекта;
- долговременная — общие факты текущего browser-сеанса.

Перед каждым ответом backend заново строит system prompt из базовых правил,
общей и проектной памяти. После успешного main LLM вызова выполняется memory
extractor; ответ показывается только после его завершения. При ошибке extractor
пара user/assistant сохраняется и показывается, а project/global snapshots
остаются прежними с безопасным статусом ошибки.

## Локальный запуск

```sh
cp backend/config.example.yaml backend/config.yaml
cp backend/llm.example.yaml backend/llm.yaml
# заполните api_key в backend/llm.yaml
cd backend && GOCACHE=$PWD/.gocache go run ./cmd/api-server --config=config.yaml
```

Во втором терминале:

```sh
cd frontend && cp .env.example .env.local && npm ci && npm run dev
```

Откройте [localhost:3000](http://localhost:3000).

В `backend/llm.yaml` обязательны секции `chat`, `text` и `memory`. Секция
`memory` использует [memory extractor prompt](backend/prompts/memory-extractor-system.txt)
и должна возвращать строгий JSON с `global_facts` и `project_facts`.

## Docker Compose

```sh
docker compose up --build
```

История хранится в одном JSON-файле версии 3 по `history_path`; она содержит
только данные сеанса, проекты, чаты и memory snapshots, но не credentials.
Volume `barista-history` переживает пересоздание контейнеров. `docker compose
down -v` удалит эту историю.

## Проверки

```sh
cd backend && GOCACHE=$PWD/.gocache go test -race ./... && GOCACHE=$PWD/.gocache go vet ./...
cd frontend && npm run lint && npm run typecheck && npm test && npm run build
```

- [Актуальный контракт агента](.specs/barista-agent/SPEC.md)
- [Актуальная модель памяти](.specs/memory-layers/SPEC.md)
- [Архив прежних решений](.specs/deprecated/README.md) — историческая справка, не текущий контракт.
