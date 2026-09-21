# Решения: контролируемые переходы задач

## Принятые решения

- Набор stage не меняется: `clarify_input`, `research_input_data`, `execution`,
  `user_feedback`.
- Stage изменяется только между соседними разными состояниями прямого графа
  `clarify → research → execution → feedback`. Равный stage — отсутствие
  transition при продвижении плана, а не self-transition.
- Из active `user_feedback` неположительный или уточняющий feedback может
  вернуть задачу в `clarify_input`, `research_input_data` или `execution`.
  Переход `user_feedback → user_feedback` запрещён.
- Выход из clarify требует явного пользовательского подтверждения оборудования,
  которое будет использовано. Утверждение модели не служит доказательством.
- Положительный feedback меняет lifecycle status `active → done`; stage остаётся
  `user_feedback` как историческая метка. Done терминален, paused остаётся
  lifecycle-статусом поверх последнего подтверждённого stage.
- Переход `execution → user_feedback` требует completed plan и успешного
  публичного `validation_result`. Gate работает в existing invariant pipeline
  и не добавляет отдельный provider-вызов или stage.
- `validation_result` не входит в LLM proposal и не декодируется из него.
  После успешных deterministic и semantic checks его вместе с безопасным summary
  создаёт server; неизвестное поле `validation_result` в strict proposal
  отклоняется.
- Invalid proposal, validation failure и исчерпание repair не меняют snapshot;
  после repair пользователь получает безопасное следующее действие. Resume
  повторяет последний подтверждённый шаг.

## Риски и границы

- Существующий persisted state не содержит `validation_result`; disk schema
  повышается. Legacy state до feedback получает `not_validated`, legacy
  feedback/done — дополнительно `legacy_unvalidated=true`, без backfill
  `passed`. Legacy done терминален, а следующий input legacy feedback
  возвращает её в execution для новой validation.
- Проверка «явного подтверждения оборудования» должна быть серверным
  доказуемым контрактом user input. Эвристическое утверждение LLM не подходит.
- Новая semantic проверка как отдельный provider-вызов не принята: validation
  gate обязан уложиться в действующий invariant pipeline и его budget.

## Связанные документы

- [Поведенческая спецификация](SPEC.md)
- [Инварианты состояния](../invariants/SPEC.md)
- [Базовое поведение агента](../barista-agent/SPEC.md)
