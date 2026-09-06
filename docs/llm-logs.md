# Как читать логи LLM

## Полные запросы и ответы

Включите диагностический режим из корня проекта:

```sh
LLM_LOG_PAYLOADS=true docker compose up -d --build barista-api
docker compose logs -f --since=1m barista-api
```

Теперь повторите нужную модель. По её request_id смотрите:

- `llm.request body=...` — полный JSON отправленного запроса.
- `llm.response body=...` — тело ответа, включая ошибки провайдера и reasoning.
- `complete=false` — получена лишь часть ответа (таймаут, обрыв или лимит чтения).
- `llm.finish error=...` — конкретная ошибка; `stage=await_headers` — ожидание
  заголовков, `read_body` — чтение ответа, `parse_response` — разбор JSON.
- `llm.validation` — статистика токенов, если проверка usage не прошла.

При таймауте до получения заголовков `llm.response` отсутствует: ответа ещё нет.
Переводы строк и кавычки экранируются логгером. Дополнительной обрезки тела логгером
приложения нет, но существующий лимит чтения алгоритмических ответов сохраняется.

Внимание: полные тела содержат промпты, ответы и другие личные данные.
Authorization не выводится; настроенный ключ клиента и base URL маскируются.
Другие секреты, введённые самим пользователем в промпт, автоматически не распознаются.
Не публикуйте такие логи без проверки. Отключение:

```sh
LLM_LOG_PAYLOADS=false docker compose up -d barista-api
```

Это не удаляет уже записанные логи. При локальном запуске:

```sh
cd backend
LLM_LOG_PAYLOADS=true go run ./cmd/api-server --config=config.yaml --addr=:8080
```

Логи backend идут в stderr, BFF — в серверный stdout Next.js, не в консоль браузера.
Новые логи появятся только после перезапуска процессов с обновлённым кодом.

## Docker Compose

Из корня проекта пересоберите и запустите сервисы:

```sh
docker compose up -d --build
docker compose logs -f --since=5m barista-api barista-web
```

После открытия логов нажмите «Сравнить 3 модели» один раз. Это три платных запроса.
Ctrl+C прекращает просмотр логов, но не останавливает контейнеры.
Последние записи без ожидания:

```sh
docker compose logs --tail=200 barista-api barista-web
```

## Локальные процессы

Смотрите терминалы, где запущены `go run ./cmd/api-server` и `npm run dev`.
Backend можно запустить с сохранением логов:

```sh
cd backend
go run ./cmd/api-server --config=config.yaml --addr=:8080 2>&1 | tee /tmp/aichallenge-backend.log
```

В другом терминале:

```sh
tail -f /tmp/aichallenge-backend.log
```

## Как найти конкретную ошибку

В браузере откройте DevTools → Network → запрос `model-temperature` →
Response Headers → `X-Request-ID`. У каждой из трёх моделей свой ID.
Он одинаков в BFF и backend. Поиск:

```sh
docker compose logs --since=15m barista-api barista-web | rg 'ВСТАВЬТЕ_REQUEST_ID'
```

Ожидаемая цепочка: `bff.model.start` → `llm.start` → `llm.finish` →
`model_request.finish` → `bff.model.finish`.
Backend использует key=value, BFF — JSON; время добавляет логгер/Compose.
Пример backend:

```text
WARN llm.finish request_id=... model=kimi-k3 duration_ms=30001 http_status=0 reason=timeout
```

- `model` — какая модель вызвана.
- `duration_ms` — длительность конкретного слоя, не суммируйте BFF и LLM.
- `http_status` в LLM — статус провайдера; 0 означает, что HTTP-ответ ещё не получен.
- `backend_status` в BFF — статус Go API, например 502; исходный статус ищите в LLM.
- `llm.finish reason=success` означает, что ответ разобран. Последующая
  `llm.validation reason=invalid_usage` означает, что статистика токенов не прошла проверку.
- `model_request.finish reason=success` содержит входные/выходные токены и `cost_usd`.

## Расшифровка

| Запись | Что проверять |
| --- | --- |
| `http_error`, 401 | API-ключ соответствующего провайдера |
| `http_error`, 402 | Баланс и биллинг у провайдера |
| `http_error`, 403 | Доступ аккаунта к API/модели |
| `http_error`, 400/404/422 | Идентификатор модели, доступность модели и параметры запроса; настройку base URL |
| `http_error`, 429 | Лимиты частоты/квоты/параллельности аккаунта |
| `http_error`, 5xx | Сбой провайдера |
| `timeout` | Истёк таймаут ожидания; сравнение моделей ждёт до 180 секунд, остальные сценарии используют собственные лимиты |
| `canceled` | Контекст запроса отменён |
| `dns` / `network` | DNS, TLS, сеть или proxy контейнера |
| `read_error` | Не удалось дочитать тело ответа |
| `invalid_response` | Пустой/невалидный ответ либо неподходящий формат |
| `invalid_usage` | Нет корректных входных/выходных токенов для расчёта метрик |
| `provider_not_configured` | Для Kimi не заполнен `kimi_api_key` |
| `backend_http_error` | Найдите тот же ID в backend: BFF получил ошибочный статус |
| `backend_timeout` | BFF не дождался Go API за 190 секунд |
| `backend_network_or_config` | Соединение BFF с backend или server-side URL |
| `backend_content_type` / `backend_invalid_response` / `backend_empty_answer` | Неверный формат ответа Go API, возможно несовместимые версии контейнеров |

Статус подсказывает направление проверки, но сам по себе не всегда доказывает причину.
Сравнение использует отдельный `model_request_timeout` (по умолчанию и максимум 180s).
BFF ждёт 190 секунд, включая чтение ответа. Общий `request_timeout` не влияет
на сравнение. После изменения YAML перезапустите backend.
DeepSeek в сравнении получает thinking.type=disabled, Kimi K3 — reasoning_effort=low.
Полное отключение reasoning у Kimi K3 недоступно; это явно указано в UI.

В обычном режиме промпты, ответы, API-ключи, Authorization, URL и сырые ошибки провайдера намеренно
не пишутся в эти диагностические логи. Для разбора проблемы достаточно прислать
цепочку записей с одним request_id. Логи не запускают повторных запросов.
