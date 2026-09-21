# Прогресс: контролируемые переходы задач

## Done

- Проведено техническое интервью; решения пользователя зафиксированы в
  [DECISIONS.md](DECISIONS.md).
- Обновлена durable-спецификация жизненного цикла и создано co-located
  техническое задание без queue ID.
- Синхронизирован контракт task transition/final-validation guard со
  [спецификацией инвариантов](../invariants/SPEC.md).
- Реализованы строгий граф переходов, server-owned `validation_result`,
  state-schema migration, серверное подтверждение оборудования и отображение
  результата в API/BFF UI-контракте.
- Обновлены controlled acceptance fixtures: положительный trace проходит
  `clarify → research → execution → user_feedback → done`; persisted feedback
  содержит `validation_result=passed` только после mandatory gate.
- Добавлены проверки отклонения `validation_result` в LLM proposal, отсутствия
  fabricated `passed` в domain apply и нейтрального legacy marker при миграции.

## Next

1. Владелец проекта при необходимости принимает отдельное решение о включении
   queue tracking или иной системе трекинга.
2. Независимый QA-исполнитель готовит тестовые сценарии после появления
   реализации и проверяет все AC-TSM-01—AC-TSM-18.

## Blockers

- **Gap-TSM-01 — owner: Human.** В репозитории нет валидного `.tasks/QUEUE`.
  Это блокирует queue-prefixed task tracking; ключ очереди нельзя угадывать или
  создавать в рамках этого задания.

## Verification

- Коррекция исторической handoff-записи: утверждение «исходники, тесты,
  конфигурация, runtime-данные и Git-ветка не менялись» описывало подготовку
  спецификации до реализации. Финальный diff реализации меняет исходники и
  тесты; runtime-данные и Git-ветка не менялись. `backend/config.yaml`
  игнорируется Git и в финальный diff не входит.

- Выполнена документационная cross-check сверка `task-state-machine`,
  `invariants` и `barista-agent`: transition/final-validation владеет
  `task-state-machine`, lifecycle repair/gate — `invariants`; противоречащих
  правил в базовой спецификации не найдено.
- `git diff --check` — PASS; обязательные ac-section-якоря присутствуют, а
  ссылки на затронутые спецификации резолвятся.
- Повторная cross-check сверка закрыла legacy migration и source-of-truth
  контракты: `validation_result` создаёт только server, а AC-TSM-18 задаёт
  восстановление pre-gate state без backfill `passed`.
- Историческая проверка этапа подготовки: исходники, тесты, конфигурация,
  runtime-данные и Git-ветка не менялись на момент подготовки спецификации.
- `GOCACHE=/Users/georgekornilov/Developer/aichallenge/.gocache go test ./...`
  — PASS (запуск потребовал разрешённый loopback `httptest`).
- Frontend: `npm run lint`, `npm run typecheck`, `npm test`, `npm run build` —
  PASS.
- Docker Compose smoke: `docker compose up --build -d`, затем `curl --fail`
  к `/` и `/api/projects` на `127.0.0.1:3000` — PASS; API healthy. Реальные
  LLM task-input вызовы не выполнялись.
- Последующий end-to-end BFF smoke выполнил один реальный task input (менее 10
  provider-вызовов): подтверждён `clarify_input` snapshot с
  `validation_result=not_validated`. После пересборки Compose поиск в логах не
  нашёл канарейку user input и полей `payload`/`text`; runtime journal теперь
  всегда исключает raw provider/user/assistant content.
