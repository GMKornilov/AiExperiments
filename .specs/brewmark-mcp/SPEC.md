# BrewMark MCP: каталог кофейного оборудования

## Назначение, scope и владение

Эта спецификация — единственный SSOT отдельного удалённо размещаемого
BrewMark MCP-сервера и его read-only интеграции с основным приложением. Сервер
даёт MCP-клиентам доступ к публичному каталогу кофейного оборудования BrewMark;
frontend даёт пользователю возможность проверить registry через backend
приложение. Владелец внешнего MCP-, backend HTTP- и browser BFF-контрактов,
нормализации каталога, upstream-ошибок, конфигурации, observability и приёмки —
эта спецификация. AI-бариста, чаты, проекты и их данные по-прежнему определяет
[barista-agent](../barista-agent/SPEC.md): вкладка MCP не меняет их flow и не
передаёт им данные.

В v1 сервер предоставляет ровно четыре инструмента:

- `brewmark_list_grinders`;
- `brewmark_list_brewers`;
- `brewmark_list_filters`;
- `brewmark_list_brew_methods`.

Инструменты читают только публичные catalog endpoints BrewMark. Они не
создают, не изменяют и не удаляют BrewMark-данные и не используют
пользовательское оборудование, профили, рецепты или журналы заваривания.
`brewmark_api_status` не существует.

Внешними нормативными источниками являются [BrewMark API Reference](https://brewmark.io/developers/api-docs)
для upstream-каталога и [MCP Streamable HTTP, 2025-11-25](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)
для транспорта. Если BrewMark меняет публичную схему, изменение этого
контракта требует отдельного обновления данной спецификации.

<!-- ac-section: core-flow -->
## Основной сценарий и MCP-транспорт

Оператор размещает независимый HTTP-сервер. MCP-клиент отправляет JSON-RPC
request на единый endpoint `POST /mcp`, выполняет MCP `initialize`, после чего
может выполнить `ping`, `tools/list` и `tools/call`. Сервер работает stateless:
не создаёт MCP-сессии, не возвращает `MCP-Session-Id`, не требует сохранённого
состояния между запросами и не открывает standalone SSE stream. Каждый request
получает обычный JSON response с `Content-Type: application/json`.

`GET /mcp` существует как часть Streamable HTTP surface, но поскольку v1 не
предоставляет SSE, всегда возвращает `405 Method Not Allowed`. Иные методы на
`/mcp` также возвращают `405`. Неизвестный путь возвращает `404` и не
раскрывает конфигурацию или upstream-детали. `GET /healthz` возвращает `200`
только если процесс готов принимать HTTP-запросы; он не вызывает BrewMark и
остаётся `200` при недоступности BrewMark.

`tools/list` возвращает статический список ровно из четырёх инструментов выше
в алфавитном порядке имён. Он не обращается к BrewMark. Server capability не
объявляет `listChanged` как `true` и не посылает notification об изменении
списка.

| Имя | Описание в `tools/list` |
|---|---|
| `brewmark_list_brew_methods` | `List BrewMark brew methods.` |
| `brewmark_list_brewers` | `List BrewMark brewing machines and manual brewers.` |
| `brewmark_list_filters` | `List BrewMark coffee filters.` |
| `brewmark_list_grinders` | `List BrewMark coffee grinders.` |

Минимальный диагностический MCP-клиент является внешним deliverable Day 16. Он
получает `BREWMARK_MCP_URL` через server-side конфигурацию, устанавливает
Streamable HTTP connection, выполняет `initialize`, затем `ping`, если
используемый SDK его поддерживает, и `tools/list`. При успехе он печатает в
лексикографическом порядке имена и описания ровно четырёх tools. При connection
или protocol error он завершает работу с ненулевым кодом и безопасным
сообщением; он не вызывает BrewMark tools и не является MCP-инструментом.

### Вкладка MCP, BFF и backend-контракт

Основной frontend показывает рядом с текущей поверхностью AI-бариста отдельную
вкладку `MCP`. При открытии вкладки пользователь видит объяснение назначения и
доступную кнопку «Подключиться и получить tools». Нажатие создаёт один
same-origin запрос `POST /api/mcp/tools` без пользовательского URL, тела с
параметрами подключения или credentials. Route Handler является browser BFF:
он передаёт в backend приложения один bodyless `POST /api/mcp/tools` по
server-only `BARISTA_BACKEND_URL`, не передавая browser body, URL MCP или
credentials. Только backend читает `BREWMARK_MCP_URL`, устанавливает Streamable
HTTP connection, последовательно выполняет `initialize` и `tools/list`, после
чего закрывает клиентское соединение. Browser и frontend server никогда не
обращаются к MCP endpoint или BrewMark напрямую. `ping`, `tools/call` и
обращения к BrewMark catalog endpoints из этого сценария не выполняются.

Backend endpoint `POST /api/mcp/tools` не принимает пользовательских
параметров, body или credentials. Он предназначен для BFF, не создаёт и не
читает пользовательское состояние, а его доступность не зависит от
browser-сеанса. BFF передаёт backend только валидный `X-Request-ID`; если
browser не прислал допустимый идентификатор, BFF создаёт его. Backend использует
этот идентификатор для связанных событий и в обращении к MCP, если транспорт
допускает безопасный correlation header.

При успехе backend возвращает BFF, а BFF возвращает browser `200` и только
нормализованный JSON:

```json
{
  "tools": [
    { "name": "brewmark_list_brew_methods", "description": "List BrewMark brew methods." }
  ]
}
```

Для каждого элемента backend принимает от MCP только непустые JSON-string
`name` и `description`; не добавляет, не переводит, не сокращает и не
переставляет эти значения. BFF проверяет этот ответ и не добавляет и не меняет
поля. Поэтому success UI показывает имя и описание ровно из ответа `tools/list`,
а не локальные копии описаний. Прочие MCP-поля не покидают backend; корректный
пустой массив отображается как штатный empty state.

Вкладка имеет ровно состояния `idle`, `loading`, `success`, `empty` и `error`.
В `idle` доступна кнопка; в `loading` она недоступна и виден программно
определяемый статус ожидания; в `success` показаны все name+description; в
`empty` — явное сообщение, что MCP не вернул tools; в `error` — безопасное
сообщение и доступная кнопка ручного повтора. После `success` или `empty`
пользователь может выполнить новую явную попытку той же кнопкой. Автоматических
повторов, периодического обновления и фоновых соединений нет.

Ошибочный backend- и BFF-ответ содержит только
`{ "code": BFFErrorCode, "message": string }` и возвращает `503` для
`MCP_NOT_CONFIGURED` или `MCP_UNAVAILABLE`, `504` для `MCP_TIMEOUT`, `502` для
`MCP_PROTOCOL_ERROR` или `MCP_INVALID_RESPONSE`. BFF сохраняет код и статус
корректного backend error; connection error до backend безопасно отображается
как `MCP_UNAVAILABLE`, а некорректный backend response — как
`MCP_INVALID_RESPONSE`. `message` выбирается из фиксированного безопасного
набора; URL, сетевые детали, raw backend/MCP payload и credentials в него не
попадают. Backend ограничивает ожидание одной MCP-попытки 5 s и размер каждого
принятого MCP response 64 KiB; BFF ограничивает полный запрос к backend 5 s и
размер принятого backend response 64 KiB. Превышение лимитов даёт соответственно
`MCP_TIMEOUT` и `MCP_INVALID_RESPONSE`.

<!-- ac-section: external-contract -->
## Контракт инструментов и нормализация результата

Каждый `tools/call` принимает объект `arguments`, возвращает одновременно
`structuredContent` по указанной ниже JSON-схеме и один короткий text-content:
`"Found <count> grinders"`, `"Found <count> brewers"`,
`"Found <count> filters"` или `"Found <count> brew methods"`. `count` равен
длине одноимённого массива. Text не содержит значений, непроверенных как часть
структурированного результата.

`outputSchema` каждого инструмента — `oneOf` соответствующей success schema из
таблицы и единой error schema
`{ "code": ErrorCode, "message": string, "retryable": boolean, "retryAfter"?: string }`.
`retryAfter` допустим только для `UPSTREAM_RATE_LIMITED`. `isError: false`
используется только с success branch, а `isError: true` — только с error branch.

Все строковые input-параметры до передачи upstream обрезаются по краям. Если
после trim параметр пуст, не является JSON string или длиннее 100 Unicode
символов, инструмент завершается `INVALID_ARGUMENT` без upstream-запроса.
Неуказанный опциональный параметр не передаётся в query string.

| Tool | Input schema | Upstream | `structuredContent` при успехе |
|---|---|---|---|
| `brewmark_list_grinders` | object; optional `brand`: string 1–100 Unicode-символов после trim; дополнительные свойства запрещены | `GET /api/grinders?brand=<brand>` | `{ "grinders": Grinder[], "brands": string[], "count": integer }` |
| `brewmark_list_brewers` | object; optional независимые `brand` и `brewMethod`: string 1–100 Unicode-символов после trim; дополнительные свойства запрещены | `GET /api/machines?brand=<brand>&brewMethod=<brewMethod>` | `{ "brewers": Brewer[], "brands": string[], "count": integer }` |
| `brewmark_list_filters` | пустой object; дополнительные свойства запрещены | `GET /api/filters` | `{ "filters": Filter[], "count": integer }` |
| `brewmark_list_brew_methods` | пустой object; дополнительные свойства запрещены | `GET /api/brew-methods` | `{ "methods": BrewMethod[], "count": integer }` |

`Grinder` содержит только `id` (integer), `brand` (non-empty string), `model`
(non-empty string), `minGrindIndex` (number), `maxGrindIndex` (number) и
`clicksPerFullRange` (number). `Brewer` содержит только `id` (integer), `brand`
(non-empty string), `model` (non-empty string), `brewMethod` (non-empty string)
и `defaultWaterTempF` (number). `Filter` содержит только `id` (integer), `name`
(non-empty string), `type` (non-empty string) и `description` (string; пустая
строка допустима). Поля не переименовываются, единицы не конвертируются.

BrewMark публично документирует для `GET /api/brew-methods` только массив
объектов без стабильного перечня полей. Поэтому `BrewMethod` — JSON object с
не менее чем одним string или number полем; сервер возвращает объект без
изменения имён его scalar и JSON-полей, но не принимает как результат массив,
примитив, `null` или объект с дублированными JSON-ключами. Это изолирует MCP
envelope (`methods`, `count`) от незафиксированной внутренней формы catalogue
entry и не выдумывает поля, которых нет в документации BrewMark.

Перед возвратом сервер проверяет типы каждого обязательного поля. Он удаляет
upstream envelope-поля, не входящие в схемы (`byBrand` в частности), сохраняет
порядок элементов BrewMark, а `brands` принимает только как массив непустых
строк, удаляет повторяющиеся значения с сохранением первого вхождения. Если
`brands` отсутствует, сервер формирует его из `brand` элементов возвращаемого
каталога тем же правилом. Любое несоответствие обязательной схеме, некорректный
JSON или невозможность прочитать тело — `BAD_UPSTREAM_RESPONSE`; частичный
каталог не возвращается.

Успешный пустой каталог — штатный результат: его массив пуст и `count` равен
нулю. Отсутствие auth token не меняет схему или набор данных, на который
инструмент может рассчитывать.

<!-- ac-section: states-and-errors -->
## Ошибки, безопасность и наблюдаемость

Ошибки выполнения зарегистрированного tool возвращаются MCP tool result с
`isError: true`, коротким text-content и безопасным `structuredContent`:

```json
{
  "code": "UPSTREAM_UNAVAILABLE",
  "message": "BrewMark API is temporarily unavailable",
  "retryable": true
}
```

Допустимы только следующие коды. `message` выбирается из фиксированного
безопасного набора, а не из тела или текста upstream-ошибки.

| Code | Условие | `retryable` |
|---|---|---|
| `INVALID_ARGUMENT` | Аргументы не соответствуют schema инструмента | `false` |
| `UPSTREAM_UNAVAILABLE` | DNS/TLS/network failure или HTTP 5xx BrewMark | `true` |
| `UPSTREAM_TIMEOUT` | Превышен configured upstream timeout | `true` |
| `UPSTREAM_RATE_LIMITED` | BrewMark вернул HTTP 429 | `true` |
| `BAD_UPSTREAM_RESPONSE` | HTTP success с некорректным либо несовместимым JSON | `false` |

При `UPSTREAM_RATE_LIMITED`, если upstream передал валидный `Retry-After`, он
добавляется как `retryAfter` в `structuredContent` в исходном значении заголовка;
иначе `retryAfter` отсутствует. Сервер не изобретает значение retry и не
повторяет запрос автоматически. HTTP 4xx BrewMark, кроме 429, безопасно
отображаются как `UPSTREAM_UNAVAILABLE`, если документированный контракт не
позволяет точно классифицировать причину.

Неизвестное имя tool, malformed JSON-RPC request или нарушение MCP lifecycle
возвращают protocol JSON-RPC error, а не tool result с `isError`. HTTP request
с `Origin` отсутствующим допустим для non-browser MCP clients. Если `Origin`
присутствует, он должен в точности совпасть с одним из нормализованных origins
конфигурационного allowlist; иначе сервер возвращает `403 Forbidden` до
декодирования MCP payload. Ответ `403` не раскрывает allowlist. Это не заменяет
аутентификацию MCP-клиента.

Для каждого HTTP/MCP-взаимодействия и каждого upstream-вызова журнал содержит
correlation ID, operation, outcome, безопасную error category и duration в
миллисекундах. Корреляционный ID входящего запроса используется для связанных
upstream-событий либо создаётся сервером при отсутствии корректного входящего
ID. В логах, HTTP-ответах, MCP content и metrics запрещены
`BREWMARK_API_TOKEN`, значение `Authorization`, raw request/response body,
URL, query values, filter values и иные секреты. Логи фиксируют только имя tool
и наличие/отсутствие фильтра, но не его текст.

### Системный MCP-журнал

Эта спецификация владеет отдельным runtime-журналом `mcp` в существующей
операторской поверхности `/admin`. Он global, не принимает, не выдаёт и не
удаляет `chat_id`/`dialog_id`, не перечисляет пользователей и очищается при
перезапуске backend. Чат-журнал остаётся контрактом
[barista-agent](../barista-agent/SPEC.md).

Чтение выполняется только через `GET /api/admin/logs?scope=mcp`. Это
единственный допустимый query-параметр: `dialog_id`, `action` или любой иной
параметр возвращает safe `400 validation`. Успех — `200` с
`{ "scope": "mcp", "retention": "backend_runtime", "logs": MCPLog[] }`;
оно не создаёт audit-event. Каждая `MCPLog` содержит обязательные `timestamp`
(RFC 3339 UTC), `source`, `event`, `operation`, `outcome`, `correlation_id` и
`duration_ms` (целое >= 0). `http_status` допустим только для HTTP-hop,
`error_category` только при `outcome=failure`, `tool` только для tools/call.
`source`: `frontend_bff`, `backend`, `mcp_server` или `brewmark`; `outcome`:
`started`, `success` либо `failure`. `operation` и `event` — безопасные
фиксированные имена операции, не URL. Допустимые пары `source:event/operation`:
`frontend_bff:mcp_tools_request`, `backend:mcp_tools_list`,
`mcp_server:mcp_http`, `mcp_server:mcp_initialize`,
`mcp_server:mcp_tools_list`, `mcp_server:mcp_tools_call` и
`brewmark:brewmark_request`; event и operation в каждой паре равны.

Backend владеет collector-ом `POST /api/internal/observability/mcp`. Это
private server-to-server endpoint, не проксируемый BFF в browser. Он принимает
только `Content-Type: application/json`, exact JSON schema `MCPLog` без
неизвестных полей и authenticated заголовок
`Authorization: Bearer <MCP_OBSERVABILITY_TOKEN>`. Секрет имеет server-only
конфигурацию в backend, frontend BFF и MCP service; он не попадает в browser,
логи или responses. Неверный/отсутствующий credential получает `403`,
невалидная schema — `400`; оба случая не создают запись. Принятая запись
возвращает `204` и попадает в `scope=mcp`. Collector доступен только между
compose/private services и не является public API.

Каждый BFF request `POST /api/mcp/tools`, backend `mcp_tools_list`, MCP HTTP,
`initialize` и `tools/list` публикуют связанные общим `correlation_id` записи.
Каждый фактически выполненный `tools/call` публикует MCP tool-event и ровно
один `source=brewmark` upstream-event, включая timeout и ошибочный ответ.
Поэтому registry-only пользовательский сценарий закономерно не содержит
BrewMark-вызова, а любой catalog tool-call виден в `/admin`. Backend пишет
собственные записи непосредственно; BFF и отдельный MCP process доставляют
свои safe records в collector до завершения server-side операции. Недоступность
collector-а не меняет product response, но producer создаёт failure console
record без payload и повторяет доставку не более одного раза в пределах 1 s.

В console и `mcp`-журнале запрещены `BARISTA_BACKEND_URL`,
`BREWMARK_MCP_URL`, `BREWMARK_BASE_URL`, host/path/URL, query/filter values,
headers, credentials, raw backend/MCP/BrewMark request/response, user data,
`payload` и `text` независимо от `log_text_payloads`.

<!-- ac-section: adaptive -->
## Адаптивность и доступность

На ширине viewport 390 px и 1440 px вкладка MCP, кнопка подключения, все
состояния и name+description tools не создают горизонтальную прокрутку viewport.
Длинные имена и описания переносятся без обрезания смыслового текста; при
переполнении внутри выделенного блока допускается его собственная прокрутка.
Вкладка и кнопки достижимы Tab-навигацией, запускаются Enter и Space и имеют
доступное имя. `loading`, `success`, `empty` и `error` программно объявляются
assistive technology; карточки tools используют семантический список с
name+description. Это не изменяет доступность независимых MCP-клиентов:
короткий text-result каждого server tool остаётся для них самостоятельной
машиночитаемой сводкой.

<!-- ac-section: permissions -->
## Права, конфигурация и security-границы

Конфигурация задаётся environment-переменными; `.env.example` содержит имена и
несекретные defaults, но не token:

| Variable | Правило |
|---|---|
| `BREWMARK_BASE_URL` | base URL BrewMark; по умолчанию `https://brewmark.io`; должен быть абсолютным `http` или `https` URL без query и fragment; production BrewMark URL использует `https` |
| `BREWMARK_API_TOKEN` | optional; после trim непустое значение добавляет только к upstream-запросу `Authorization: Bearer <token>`; пустое/отсутствующее значение не добавляет заголовок вовсе |
| `BREWMARK_REQUEST_TIMEOUT` | положительная длительность upstream-запроса; default `10s` |
| `MCP_ADDR` | HTTP listen address; default `127.0.0.1:8080`; Compose внутри контейнера явно переопределяет его на `:8080`, публикуя host-порт `8081`; адрес нужен независимым MCP-клиентам и backend, но не frontend |
| `MCP_ALLOWED_ORIGINS` | список разрешённых origins для присутствующего `Origin`; пустой список означает, что любой request с `Origin` отклоняется, но запрос без него допускается |
| `BREWMARK_MCP_URL` | абсолютный `http` или `https` URL MCP endpoint без query, fragment или userinfo; только server-side конфигурация диагностического клиента и backend приложения, не имеет `NEXT_PUBLIC_` префикса и не принимается от browser-пользователя |
| `BARISTA_BACKEND_URL` | абсолютный private URL backend приложения без query, fragment или userinfo; только server-side конфигурация frontend BFF, не имеет `NEXT_PUBLIC_` префикса и не принимается от browser-пользователя |
| `MCP_OBSERVABILITY_TOKEN` | непустой service credential для private collector; одинаковое server-side значение в backend, frontend BFF и MCP service, без `NEXT_PUBLIC_` префикса |

`BREWMARK_API_TOKEN` — credential сервера к BrewMark, а не credential MCP
клиента. Он не возвращается, не попадает в structured content и не может
влиять на registry tools. Клиентская authentication/authorization публичного
`/mcp`, tenant isolation, OAuth, user delegation и quotas намеренно отсутствуют
из v1: размещать endpoint без отдельной защиты допустимо только в доверенной
инфраструктуре оператора. До публичного internet-exposure это отдельный
security contract с владельцем **Human**.

<!-- ac-section: connectivity -->
## Связность, свежесть и восстановление

`initialize`, `ping`, `tools/list` и `GET /healthz` полностью локальны и
успешны при outage BrewMark. Только вызов одного из четырёх catalog tools
делает один upstream request. При network transition, timeout, 5xx или 429
инструмент возвращает соответствующую безопасную ошибку; сервер не кеширует,
не делает retry и не подменяет текущий ответ ранее прочитанным каталогом.
Следующий явный tool call создаёт новую попытку. Следовательно, успешный
результат отражает ответ BrewMark на момент конкретного вызова, без гарантии
между вызовами.

Для вкладки MCP BFF выполняет новый backend request, а backend — новую
MCP-попытку только по явному нажатию пользователя. Недоступность, timeout или
protocol error backend/MCP не влияют на текущий AI-бариста и не вызывают
обращение browser к иному URL; пользователь видит `error` и может вручную
повторить попытку. Успешный `tools/list` не означает доступность BrewMark
catalogue: backend намеренно не вызывает catalog tools.

По наблюдению 2026-09-22 live BrewMark `/api/health` отвечал `503`,
`/api/grinders`, `/api/machines` и `/api/filters` — `500`, тогда как
`/api/brew-methods` — `200`. Это зафиксированное внешнее наблюдение, не
контракт сервера и не основание ослаблять acceptance criteria: live upstream не
может быть единственным критерием готовности.

<!-- ac-section: destructive-actions -->
## Деструктивные действия

N/A с рационалом: v1 не выполняет мутации, не хранит пользовательских данных и
не имеет удаления, подтверждения или undo-сценариев.

<!-- ac-section: deep-link -->
## Deep link и intake

Вкладка MCP не имеет пользовательского URL, не принимает connection URL из
query, form или deep link и не хранит результат `tools/list` после reload.
После reload она возвращается в `idle`; новый результат получается только
явным действием пользователя. MCP-server остаётся stateless и не требует
восстановления между HTTP-запросами.

<!-- ac-section: performance -->
## Производительность и проверка

Для локальных операций `initialize`, `ping`, `tools/list` и `GET /healthz`
p95 server-side duration не превышает 100 ms при 100 последовательных запросах
на незагруженном экземпляре без upstream-вызова. Проверка: test harness
измеряет duration на серверной стороне или по монотонным логам, исключая
network latency клиента. Для catalog tool сервер прекращает upstream ожидание
не позднее configured `BREWMARK_REQUEST_TIMEOUT`; проверка: controlled delayed
mock и измерение elapsed time с допустимым scheduling tolerance 250 ms.

Unit и integration tests используют controllable local BrewMark mock для
success, empty catalogue, filter query, malformed payload, 429 с/без
`Retry-After`, timeout, network/5xx и token-present/token-absent запросов.
Тесты MCP endpoint покрывают `initialize`, `ping`, `tools/list`, каждый из
четырёх `tools/call`, malformed MCP request, unknown tool, GET `/mcp` = 405,
Origin allow/deny и независимость `/healthz` от mock outage. Они не требуют
живого BrewMark.

Тесты backend HTTP boundary покрывают корректный порядок `initialize` →
`tools/list`, передачу только name+description без вызова `tools/call`,
normalisation допустимого пустого списка, timeout,
connection/protocol/invalid-response errors, отсутствующий URL и лимит 64 KiB.
Тесты BFF используют controllable backend mock и покрывают bodyless forwarding,
корреляцию, нормализацию backend response, timeout, connection и malformed/
oversized backend response. Browser tests покрывают все пять UI states, ручной
retry без автоматической дополнительной попытки, клавиатуру/ARIA и viewport
390 px/1440 px. Они не создают соединение с MCP; live BrewMark не является
зависимостью тестов. Journal unit/integration tests проверяют strict collector
schema/auth, изоляцию `mcp` от chat ID, общий correlation ID всех synthetic
hops, включая MCP→BrewMark, и redaction с sentinel URL, filter, credential и
raw body. Browser tests проверяют выбор обоих журналов, empty/error, обновление
каждые 5 s и доступность system журнала.

Перед передачей реализации запускается актуальный Docker Compose и выполняется
полноценный smoke scenario: поднятый MCP server принимает Streamable HTTP
connection диагностического клиента, тот проходит `initialize`/доступный
`ping`/`tools/list` и детерминированно выводит ровно четыре tools; затем
`brewmark_list_grinders` против compose-visible mock возвращает нормализованный
catalog. В browser smoke пользователь открывает вкладку MCP в основном
frontend, нажимает подключение, BFF обращается к backend, backend обращается к
MCP, и пользователь видит четыре имени с описаниями. Отдельный live check
допустим только как diagnostic evidence и не заменяет этот smoke.
Smoke также открывает `/admin` в режиме «Системный MCP» и проверяет связанные
записи BFF→backend→MCP; отдельный compose-visible `tools/call` к mock BrewMark
добавляет видимое событие `source=brewmark` с тем же correlation ID.

<!-- ac-section: acceptance-criteria -->
## Acceptance criteria

- **AC-BMCP-01.** `POST /mcp` принимает корректные Streamable HTTP JSON-RPC
  `initialize`, `ping` и `tools/list`; запросы получают JSON response, не
  создают `MCP-Session-Id`, а `tools/list` возвращает в алфавитном порядке
  ровно четыре заданных инструмента без HTTP-вызова к mock BrewMark. Проверка:
  protocol integration test и счётчик mock requests.

- **AC-BMCP-02.** `GET /mcp` и любой неподдерживаемый метод `/mcp` возвращают
  405; `GET /healthz` возвращает 200 при доступном и при недоступном mock
  BrewMark. Проверка: HTTP integration test в двух состояниях mock.

- **AC-BMCP-03.** Каждый catalog tool направляет ровно один GET к указанному
  endpoint, передаёт только валидные optional filters, возвращает заданную
  нормализованную schema и согласованный `count`, `structuredContent` и text.
  `brewmark_list_brewers` обращается к `/api/machines`. Проверка: contract tests
  на fixtures всех четырёх endpoints, включая empty result.

- **AC-BMCP-04.** Невалидный аргумент не создаёт upstream request и возвращает
  tool failure `isError: true` с `INVALID_ARGUMENT`; malformed MCP, lifecycle
  violation и unknown tool возвращают JSON-RPC protocol error, а не tool
  failure. Проверка: protocol tests и счётчик mock requests.

- **AC-BMCP-05.** Timeout, network/5xx, 429 и incompatible success payload
  возвращают соответственно `UPSTREAM_TIMEOUT`, `UPSTREAM_UNAVAILABLE`,
  `UPSTREAM_RATE_LIMITED` и `BAD_UPSTREAM_RESPONSE` с `isError: true`; только
  валидный upstream `Retry-After` появляется в 429 result. Проверка: controlled
  mock fixtures и schema assertions.

- **AC-BMCP-06.** При непустом trimmed `BREWMARK_API_TOKEN` каждый upstream
  request содержит ровно `Authorization: Bearer <token>`; при пустом или
  отсутствующем token заголовок отсутствует. Token и raw Authorization не
  встречаются в MCP response или logs. Проверка: mock request capture и
  redaction-log test с sentinel token.

- **AC-BMCP-07.** Запрос без `Origin` допускается; с неразрешённым origin
  получает 403 до JSON-RPC decoding; с разрешённым origin выполняется штатно.
  Проверка: три HTTP integration cases с allowlist и malformed body.

- **AC-BMCP-08.** В событии HTTP/MCP и upstream есть correlation ID, outcome,
  error category при ошибке и duration; записи не содержат secrets, raw bodies
  или filter text. Проверка: structured-log test success/error request с
  sentinel secret и filter.

- **AC-BMCP-09.** При 100 последовательных локальных запросах p95 для
  `initialize`, `ping`, `tools/list` и `/healthz` не превышает 100 ms; timeout
  завершает catalog call не позже configured limit + 250 ms. Проверка:
  reproducible performance harness и delayed mock.

- **AC-BMCP-10.** Диагностический клиент с заданным `BREWMARK_MCP_URL` делает
  `initialize`, доступный SDK `ping` и `tools/list`, печатает в
  лексикографическом порядке имена и описания ровно четырёх tools и завершает
  работу с нулевым кодом. При connection/protocol error он не вызывает tools и
  завершается с ненулевым кодом. Проверка: Compose smoke с работающим сервером
  и отдельные failure fixtures.

- **AC-BMCP-11.** Вкладка `MCP` рядом с AI-бариста по явному нажатию делает
  один same-origin `POST /api/mcp/tools` без body. BFF направляет один bodyless
  `POST /api/mcp/tools` в backend по server-only `BARISTA_BACKEND_URL`; backend
  по server-only `BREWMARK_MCP_URL` выполняет строго `initialize`, затем
  `tools/list`. Browser и frontend server не делают запрос к MCP endpoint или
  BrewMark напрямую. При ответе registry вкладка показывает четыре
  name+description в порядке и с текстом ответа MCP. Проверка: browser network
  trace, BFF/backend/MCP controllable mocks с capture HTTP hops и JSON-RPC
  methods; Compose browser smoke «вкладка → BFF → backend → MCP → 4 tools».

- **AC-BMCP-12.** Вкладка реализует `idle`, `loading`, `success`, `empty` и
  `error`: pending action недоступно в `loading`; `empty` допустим только при
  корректном пустом списке; error показывает безопасное сообщение и ручной
  retry; никакое состояние не создаёт automatic retry или background request.
  BFF и backend не принимают URL от пользователя; backend возвращает только
  name+description либо установленный safe error, BFF проверяет и передаёт этот
  контракт. Каждая backend MCP-попытка и BFF backend-request завершается не
  позднее 5 s, а ответ каждого hop больше 64 KiB отклоняется как
  `MCP_INVALID_RESPONSE`. На 390 px и 1440 px controls и все состояния доступны
  с клавиатуры и программно объявляют status. Проверка: controlled browser/BFF/
  backend tests для states, retry, network trace, timeout, oversized и
  malformed backend/MCP response, keyboard/ARIA и двух viewport.

- **AC-BMCP-13.** Каждая BFF-, backend- и MCP-попытка для одного нажатия имеет
  общий correlation ID, operation, outcome, error category при ошибке и
  duration. Ни одна запись не содержит backend/MCP URL, credentials, raw
  backend/MCP payload или user data. Проверка: structured-log tests success и
  каждого failure hop с sentinel URL, credential и user data.

- **AC-BMCP-14.** `GET /api/admin/logs?scope=mcp` возвращает только global
  runtime MCP records без chat/dialog ID; `action`, `dialog_id`, иной query или
  scope возвращает `400 validation`, а чтение не добавляет event. Каждый record
  соответствует `MCPLog`: обязательные поля заполнены, `http_status` есть
  только у HTTP-hop, `error_category` — только у failure. Проверка: backend
  HTTP contract + BFF forwarding + journal unit tests.

- **AC-BMCP-15.** Private collector принимает exact authenticated `MCPLog` и
  отвечает `204`; неверный token получает `403`, malformed/unknown field —
  `400`, и ни один из отказов не сохраняет record. Token не встречается в
  response/console/journal. Проверка: collector integration с sentinel token и
  schema fixtures.

- **AC-BMCP-16.** Controlled trace registry click показывает в `scope=mcp`
  связанные общим correlation ID BFF, backend, MCP HTTP, `initialize` и
  `tools/list` events. Controlled catalog tools/call дополнительно показывает
  ровно один `source=brewmark` event, включая upstream timeout/error. Sentinel
  URL, query/filter value, authorization, credential и raw body отсутствуют из
  console и `/admin` при любом `log_text_payloads`. Проверка: producer/collector
  integration и redaction tests.

- **AC-BMCP-17.** `/admin` переключает «Журнал чата / Системный MCP»; второй
  режим не требует chat ID, скрывает фильтр «Только LLM» и показывает timestamp,
  source, operation/event, outcome, correlation ID, duration, HTTP status при
  наличии и category при failure. Loading, empty, read error и обновление
  программно объявляются; выбор и refresh доступны с клавиатуры, а на 390 px и
  1440 px нет viewport overflow. Проверка: component/browser accessibility и
  Compose browser smoke.

## Out of scope и открытые gaps

- Интеграция MCP tools с chat/task flow AI-бариста не входит в v1: вкладка
  показывает только registry и не передаёт tools, каталог или состояние в чат.
- Recipe adaptation, coffee/roaster search, пользовательские коллекции,
  сохранённое оборудование и любые write endpoints BrewMark не входят в v1.
- SSE, MCP sessions, resumability, server-to-client notifications и динамичный
  список tools не входят в v1.
- Публичная client authentication/authorization `/mcp`, OAuth и rate limiting
  не входят в v1. **Gap-BMCP-01 — owner: Human.** До размещения endpoint в
  недоверенной публичной сети нужно выбрать механизм клиентской аутентификации
  и владельца политики доступа; это блокирует такой deployment, но не локальный
  или доверенный hosting v1.
- Точный стабильный schema BrewMark brew-method entry не опубликован. В v1
  используется защищённый object passthrough, описанный выше. **Gap-BMCP-02 —
  owner: Human.** Нужен официальный upstream-контракт полей, если клиентам
  понадобятся типизированные поля brew method; это не блокирует выдачу
  catalogue objects.
