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
cp -n backend/config.example.yaml backend/config.yaml
cp -n backend/llm.example.yaml backend/llm.yaml
cp -n .env.example .env
# заполните SECURE_API_KEY в .env
cd backend && GOCACHE=$PWD/.gocache go run ./cmd/api-server --config=config.yaml --env-file=../.env
```

Во втором терминале:

```sh
cd frontend && cp .env.example .env.local && npm ci && npm run dev
```

Откройте [localhost:3000](http://localhost:3000).

В `backend/llm.yaml` обязательны секции `chat`, `text` и `memory`. Секция
`memory` использует [memory extractor prompt](backend/prompts/memory-extractor-system.txt)
и должна возвращать строгий JSON с `global_facts` и `project_facts`.

Значения вида `${NAME}` в backend YAML подставляются из окружения до разбора
конфигурации. Для локальных секретов используйте `.env` (он не попадает в Git)
и передайте его повторяемым флагом `--env-file`: например,
`--env-file=.env --env-file=deploy/secrets.env`. Переменные окружения процесса
имеют приоритет над dotenv-файлами; среди dotenv-файлов побеждает первое
определение переменной. Неопределённый или пустой placeholder останавливает
запуск безопасной config error без раскрытия значения.

## Docker Compose

```sh
cp -n backend/config.example.yaml backend/config.yaml
cp -n backend/llm.example.yaml backend/llm.yaml
cp -n .env.example .env
# заполните SECURE_API_KEY в .env
docker compose up --build -d
```

`barista-api` получает корневой `.env` как runtime environment; файл не
копируется в image и не монтируется в контейнер. Поэтому `${SECURE_API_KEY}` в
смонтированном `backend/llm.yaml` раскрывается при запуске backend. Значения,
переданные в environment контейнера, имеют обычный приоритет над dotenv-файлами
самого `api-server`.

Для отдельного deployment-файла укажите путь до запуска Compose:

```sh
BARISTA_ENV_FILE=deploy/production.env docker compose up --build -d
```

Такой файл должен быть доступен Docker Compose на host и не должен храниться в
Git. Конфигурационный YAML и prompts монтируются read-only; backend не
публикует порт наружу, а web обращается к нему по внутренней сети Compose.
Контейнеры перезапускаются после сбоя (`unless-stopped`), а web ждёт успешный
`/healthz` backend. Для обновления используйте `docker compose up --build -d`;
для просмотра состояния — `docker compose ps` и `docker compose logs -f`.

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
