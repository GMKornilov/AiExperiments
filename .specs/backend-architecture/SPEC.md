# Архитектура backend

Активное поведение определяется спецификациями barista-agent, memory-layers,
personalization и task-state-machine. Этот документ фиксирует внутренние границы
рефакторинга из docs/backend-refactoring-plan.md, не заменяя их AC.

- HTTP декодирует transport, вызывает отдельные workspace/conversation/taskflow
  use cases и централизованно отображает безопасные ошибки.
- Domain содержит типизированное состояние и инварианты без I/O. JSON DTO живут
  на границах, application не импортирует concrete adapters.
- Хранилище публикует copy-on-write candidate только после успешного сохранения.
  Внешние вызовы выполняются без state lock. Отмена побеждает поздний результат.
- Обычная беседа сохраняет pair при ошибке extractor с прежней памятью.
  Task step принимает pair, память, proposal и план только одним commit.
- Title имеет независимый durable claim при первом принятом вводе; restart
  переводит незавершённый claim в fallback без повторного вызова.
- Task proposal декодируется из того же completion; отдельного вызова для плана
  нет. Идемпотентность использует optional client_message_id, сохранённый клиентом
  до отправки и повторяемый при ручном retry.
- Первый browser request устанавливает session cookie до загрузки профилей и
  пользовательских mutations. Параллельные ответы без исходной cookie не должны
  переключать владельца уже созданного проекта. Проверка: fresh browser context,
  cookie в первом profile request, затем создание проекта/чата без not_found.
- Актуальные JSON v3/v4/v5 поддерживаются. Неизвестные поля, версии и повреждённые
  данные приводят к безопасному отказу без перезаписи. Historical v1/v2 reset
  допускается только после проверки формы и backup, включая каталог диалогов.

Приёмка: import graph, общий storage contract для fake/JSON, controlled failure,
cancellation/restart/ordering fixtures, API/browser проверки и isolated Compose
smoke. Прогресс и фактические результаты — docs/backend-refactoring-progress.md.
