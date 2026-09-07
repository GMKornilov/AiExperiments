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

При базовой проверке контейнеры не собирались; Compose проверен на корректность конфигурации.

## Дополнение: ширина, ID и отдельная модель названий

Изменения от 2026-09-07. LLM YAML разделён на обязательные объекты `chat` и `text`.
Название генерируется отдельным запросом после первого принятого вопроса;
до результата или при ошибке используются первые 60 Unicode-символов вопроса.

На работающем backend с управляемым HTTP provider подтверждены:

- Разные endpoint, credential, model и prompt для основного ответа и названия.
- Медленное название не задерживает основной ответ и следующее сообщение.
- Дедупликация и retry основного ответа не запускают название повторно.
- Ошибка модели названия сохраняет fallback и возможность продолжать переписку.
- Удаление во время генерации не допускает восстановления диалога или журнала.
- Оба credential скрываются в текстовых логах основного ответа и названия.
- Старый диалог сохраняет оба снимка LLM, новый использует обновлённые настройки.

Добавлены постоянные lifecycle-тесты в `backend/internal/session/title_test.go`,
проверка двух credential в `backend/internal/observability/title_test.go`,
component-тесты копирования ID и доступности composer при pending title.

Итоговые проверки дополнения:

- `go test -race -timeout 60s ./...` и `go vet ./...` — пройдены.
- Отдельно воспроизведён HTTP timeout вспомогательной модели: в журнале
  `title_error` с категорией `timeout`, основной ответ успешен.
- Frontend lint, typecheck, 16 Vitest и production build — пройдены.
- Chrome E2E: 5/5; включая точное содержимое clipboard, полную ширину,
  pending title с доступным composer и появление названия через polling.
- Docker images пересобраны; оба сервиса на `localhost:3000` — healthy.
- Smoke в пересобранном Docker-приложении: внешняя область 1440 px при viewport
  1440 px (вместо ограничения 880 px), clipboard содержит точный ID;
  при ширине 320 px горизонтального переполнения нет. Скриншоты просмотрены.
- Реальный ignored `backend/llm.yaml` мигрирован; parsed `chat` полностью совпадает
  с исходной резервной копией `/private/tmp/aichallenge-llm-before-title.yaml`.
  В `text` выбрана `deepseek-v4-flash`, timeout 30 секунд и отдельный prompt.

Проверки генерации выполнялись с fake provider, платный LLM API не вызывался.

## Дополнение: температура моделей

Добавлены независимые `chat.temperature` и `text.temperature`: конечное число
от 0 до 2, default 1 при отсутствии поля. Явный 0 включается в JSON-запрос.
Проверены YAML default, границы, null, нечисловые/неконечные и недопустимые значения;
захват HTTP-запросов подтверждает разные температуры для chat и text.
Отдельный regression-тест подтверждает сохранение старых температур 0.7/0 после
изменения YAML и использование 0.3/1.2 новым диалогом.

`go test -race -timeout 60s ./...` и `go vet ./...` прошли. Backend Docker image
пересобран и применён; создание и удаление чата через BFF на `localhost:3000`
прошли без вызова реального LLM. В локальный YAML добавлены chat=1, text=0.2;
остальные настройки сохранены (сравнение с
`/private/tmp/aichallenge-llm-before-temperature.yaml` без вывода секретов).
