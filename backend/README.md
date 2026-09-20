# Backend AI-бариста

Backend реализует проекты с чатами и три слоя памяти в рамках `X-Session-ID`:
история текущего чата, project facts и global facts. Другой browser-сеанс не
может читать или менять эти данные.

## Запуск

```sh
cp -n config.example.yaml config.yaml
cp -n llm.example.yaml llm.yaml
cp -n ../.env.example ../.env
# заполните SECURE_API_KEY в ../.env
GOCACHE=$PWD/.gocache go run ./cmd/api-server --config=config.yaml --env-file=../.env
```

`config.yaml` задаёт `addr`, `llm_config_path` и `history_path`. `llm.yaml`
содержит обязательные LLM-секции:

- `chat` — основной ответ;
- `text` — конфигурационный endpoint, сохранённый в runtime snapshot;
- `memory` — extractor с отдельным system prompt.

`context_window_messages` задаёт положительное N и по умолчанию равен 10.
`memory.system_prompt_path` должен указывать на
`prompts/memory-extractor-system.txt`. Extractor получает прежние snapshots и
последний обмен как данные, возвращает только JSON object с массивами
`global_facts` и `project_facts`; оба массива заменяются согласованно.

Строки YAML вида `${NAME}` получают значение переменной окружения `NAME` до
разбора конфигурации. Для локальных секретов используйте корневой `.env` и
передавайте его флагом `--env-file`; флаг можно повторять. Переменные процесса
имеют приоритет, а между dotenv-файлами приоритет у первого определения.
Незаданная либо пустая переменная прерывает запуск безопасной config error без
значения секрета.

## Docker Compose deployment

Из корня репозитория создайте отсутствующие `backend/config.yaml`,
`backend/llm.yaml` и корневой `.env` из соответствующих `*.example` файлов,
заполните секрет и затем выполните:

```sh
docker compose up --build -d
```

Compose передаёт `.env` в environment `barista-api`, поэтому placeholder в
смонтированном `llm.yaml` разворачивается в контейнере. `.env` не попадает в
image. Чтобы использовать другой dotenv-файл, задайте путь на host:

```sh
BARISTA_ENV_FILE=deploy/production.env docker compose up --build -d
```

Не передавайте `--env-file` в команду backend внутри контейнера: для Compose
значения уже находятся в environment процесса. Данные истории сохраняются в
именованном volume `barista-history`; удаление volume (`docker compose down -v`)
необратимо удаляет их.

## Runtime-поток

1. Backend загружает global/project memory и N сообщений выбранного чата.
2. Собирает новый main system prompt, где memory размечена как данные, а не инструкции.
3. Вызывает main LLM.
4. Вызывает memory extractor и сохраняет user/assistant pair вместе с валидными snapshots.

Если extractor недоступен или его JSON невалиден, pair сохраняется, snapshots
не меняются, а клиент получает response с `memory_status: "error"` и безопасной
категорией. Если основной LLM вызов завершился ошибкой, user-реплика сохраняется
как error и может быть повторена вручную без дублирования пары.

Task flow выполняется отдельно от обычной беседы. Один LLM-вызов предлагает
ровно один task step. Если `expected_action` начинается с `agent:`, backend
автоматически передаёт новый task snapshot и предыдущие outputs следующему
вызову. Цепочка останавливается на `user:`, `done`, ошибке либо после восьмого
вызова. Затем один memory extractor обрабатывает совокупный результат, а output,
memory и конечный proposal/plan принимаются одним commit. При любой ошибке
остаётся последний подтверждённый шаг. `client_message_id`
у task input и Resume опционален для старых клиентов и обязателен для
идемпотентного повтора новым клиентом. Автоматических retries нет.

Title запускается независимо после durable claim первого ввода. При restart
незавершённый title получает fallback, а незавершённый task — paused.

## Архитектура

- `domain/model`: сущности и инварианты без I/O;
- `application/workspace`, `conversation`, `taskflow`: отдельные use cases;
- `application/state`: single-process copy-on-write commit и владение отменой;
- `adapters/statejson`, `openai`, `extractjson`, `configsnapshot`: границы I/O;
- `httpapi`: transport DTO, endpoint interfaces и безопасные errors;
- `cmd/api-server`: композиция и lifecycle.

[Инвентарь legacy](../docs/backend-refactoring-inventory.md) отделяет сохранённые
исторические тесты от активного контура.

## HTTP API

Активный API начинается с `/api/projects`: проекты содержат `/chats`, а память
доступна через `/memory`, `/memory/global` и `/memory/project`. Mutating и
reading requests требуют `X-Session-ID`. Выбор проекта или чата, очистка и
удаление подтверждаются UI; backend возвращает только состояние владельца
сессии и безопасные error categories.

`history_path` — единый JSON state-файл: актуальные v3/v4/v5 читаются,
v3 нормализуется в v4, v5 сохраняет версию. Неизвестные поля/version и
повреждённые данные блокируют запуск без перезаписи файла. Любая нормализация
сначала сохраняет точную копию исходного JSON. Только распознанные historical
v1/v2 сбрасываются после backup JSON и переименования legacy-каталога в backup.
Credentials не входят в новое persisted state.

Логи операций и LLM-циклов содержат correlation ID, IDs проекта/чата, результат,
категорию и длительность; raw сообщения, facts и секреты не логируются.

## Проверки

```sh
GOCACHE=$PWD/.gocache go test -race ./...
GOCACHE=$PWD/.gocache go vet ./...
# Из корня репозитория: isolated Docker + browser/restart smoke
./frontend/e2e/compose-smoke.sh
```

Актуальные требования: [агент](../.specs/barista-agent/SPEC.md) и
[слои памяти](../.specs/memory-layers/SPEC.md). Предыдущие стратегии контекста
и связанные решения лежат в [deprecated archive](../.specs/deprecated/README.md)
только как справочный материал.
