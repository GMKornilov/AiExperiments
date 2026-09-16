# Backend AI-бариста

Backend реализует проекты с чатами и три слоя памяти в рамках `X-Session-ID`:
история текущего чата, project facts и global facts. Другой browser-сеанс не
может читать или менять эти данные.

## Запуск

```sh
cp config.example.yaml config.yaml
cp llm.example.yaml llm.yaml
# заполните api_key в llm.yaml
GOCACHE=$PWD/.gocache go run ./cmd/api-server --config=config.yaml
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

## Runtime-поток

1. Backend загружает global/project memory и N сообщений выбранного чата.
2. Собирает новый main system prompt, где memory размечена как данные, а не инструкции.
3. Вызывает main LLM.
4. Вызывает memory extractor и сохраняет user/assistant pair вместе с валидными snapshots.

Если extractor недоступен или его JSON невалиден, pair сохраняется, snapshots
не меняются, а клиент получает response с `memory_status: "error"` и безопасной
категорией. Если основной LLM вызов завершился ошибкой, user-реплика сохраняется
как error и может быть повторена вручную без дублирования пары.

## HTTP API

Активный API начинается с `/api/projects`: проекты содержат `/chats`, а память
доступна через `/memory`, `/memory/global` и `/memory/project`. Mutating и
reading requests требуют `X-Session-ID`. Выбор проекта или чата, очистка и
удаление подтверждаются UI; backend возвращает только состояние владельца
сессии и безопасные error categories.

`history_path` — единый JSON state-файл версии 3. При переходе со старого
формата legacy-чаты не мигрируются: старая история и связанная папка удаляются,
новая модель начинает с пустого состояния. Файл не содержит credentials.

Логи операций и LLM-циклов содержат correlation ID, IDs проекта/чата, результат,
категорию и длительность; raw сообщения, facts и секреты не логируются.

## Проверки

```sh
GOCACHE=$PWD/.gocache go test -race ./...
GOCACHE=$PWD/.gocache go vet ./...
```

Актуальные требования: [агент](../.specs/barista-agent/SPEC.md) и
[слои памяти](../.specs/memory-layers/SPEC.md). Предыдущие стратегии контекста
и связанные решения лежат в [deprecated archive](../.specs/deprecated/README.md)
только как справочный материал.
