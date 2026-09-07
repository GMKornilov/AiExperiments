# Проверка реализации

Дата: 2026-09-07. Проверки выполнены с управляемым fake OpenAI-compatible provider;
реальный платный LLM API не вызывался.

## Автоматические проверки

- Backend: `go test -race -timeout 60s ./...`, `go vet ./...`, сборка `cmd/api-server` — пройдены.
- Frontend: `npm run lint`, `npm run typecheck`, `npm test` — 14 тестов пройдены.
- Production: `npm run build` — пройдена сборка Next.js.
- Browser: `npm run test:e2e` — 4 сценария пройдены в Chrome.
- `docker compose config --quiet` и `git diff --check` — пройдены.

Постоянные тесты находятся в `backend/internal/{config,agent,llm,session,httpapi,observability}`,
`backend/cmd/api-server`, `frontend/src/` и `frontend/e2e/`.
Инструкция запуска browser-стенда: [frontend/e2e/README.md](../../frontend/e2e/README.md).

## Дополнительные сквозные проверки

Ниже сценарии, выполненные основной сессией на работающем Go backend и production Next.js.

| Сценарий | Результат |
| --- | --- |
| Создание, отправка, копирование и refresh | История восстановлена, скопирован исходный текст |
| Ошибка провайдера и retry | Composer заблокирован после ошибки; retry оставляет одну user-реплику |
| Потеря успешного ответа | Перед retry прочитано состояние сервера; второго POST/upstream нет |
| Потеря ответа при pending | Восстановлен pending, затем ответ через polling; параллельного upstream нет |
| Offline до принятия | Локальная ошибка видна; после refresh непринятая реплика исчезает |
| Изоляция двух браузеров | Чужой диалог недоступен chat API; admin по точному ID доступен |
| Удаление во время ожидания | Запрос отменён, сеанс разблокирован; поздний ответ не восстанавливает диалог и журнал |
| Изменение LLM YAML и prompt | Старый диалог использует исходный снимок, новый — обновлённый |
| Невалидный LLM YAML | Новый диалог не создаётся; старый продолжает отвечать |
| Timeout снимка | Ошибка timeout; fake provider получил ровно один вызов |
| Контекст | Порядок system, user, assistant, user подтверждён захватом upstream-запроса |
| Unicode | Браузер принимает 4 000 emoji; title содержит первые 60 code points |
| Адаптивность | Чат и admin не расширяют страницу при ширине 320 px; desktop проверен при 1440 px |
| Текстовые логи выключены | User/assistant-текст, system prompt и credential отсутствуют в console/admin |
| Текстовые логи включены | User/assistant-тексты видны, credential заменён, system prompt отсутствует; индикатор admin виден |
| Устаревший env-флаг | `LLM_LOG_PAYLOADS=true` не переопределяет backend YAML |
| Admin polling | Чтение не добавляет записи; повторный lookup неизвестного ID остаётся нейтральным |
| Перезапуск backend | Для того же session ID список диалогов пуст, прежний admin ID не найден |
| Browser network | Браузер обращается только к same-origin BFF; прямых запросов к backend/provider нет |

## Локальная конфигурация

Прежний ignored `backend/config.yaml` перенесён в пару `config.yaml` / `llm.yaml`.
Сравнение с резервной копией подтвердило сохранение endpoint, credential, модели и timeout
без вывода их значений. Резервная копия: `/private/tmp/aichallenge-config-legacy-backup.yaml`.

Контейнеры не собирались и не публиковались; Compose проверен на корректность конфигурации.
