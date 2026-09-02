## Go

При работе с Go-кодом необходимо следовать [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md).

### Структура Go-проекта

Необходимо придерживаться стандартного Go layout внутри `backend/`; корень monorepo не содержит Go module или source:

- точки входа приложений размещать в `backend/cmd/<имя-команды>`;
- приватный прикладной код размещать в `backend/internal/<имя-пакета>`;
- код и assets, используемые только одним внутренним сервисом, хранить внутри соответствующего `internal`-пакета;
- `cmd`-пакеты оставлять тонкими: разбор параметров, сборка зависимостей, запуск и корректное завершение;
- module-файлы, backend-конфигурация и build/deploy-файлы Go хранить в `backend/`; корень monorepo оставлять для общих документации, спецификаций и orchestration-файлов;
- перед добавлением нового корневого каталога проверять, нельзя ли разместить его в `cmd`, `internal` или другом уже существующем стандартном каталоге.

## Спецификации

Перед началом любой задачи на реализацию необходимо создать или актуализировать соответствующую спецификацию согласно project workflow. Это правило не относится к research, анализу, ответам на вопросы и другим задачам без изменения реализации.

## Frontend

Полноценный frontend необходимо реализовывать как отдельное приложение в `frontend/` на актуальной стабильной версии Next.js, следуя официальной документации Next.js.

- Использовать App Router, TypeScript в strict-режиме и `src/` layout: routes в `src/app`, feature-код в `src/features`, общие компоненты в `src/components`, инфраструктурный код в `src/lib`.
- `page.tsx` и `layout.tsx` оставлять Server Components по умолчанию; добавлять `'use client'` только в минимальные интерактивные leaf-компоненты.
- Для browser-to-backend интеграции использовать Route Handlers как BFF-слой; не обращаться из браузера напрямую к приватному backend URL.
- Секреты и приватные backend URLs хранить только в server-side env без префикса `NEXT_PUBLIC_`; server-only модули помечать `import 'server-only'`.
- Валидировать входные данные Route Handlers, ограничивать размер и время запросов, не возвращать пользователю внутренние ошибки или секреты.
- Не использовать `dangerouslySetInnerHTML` для API-данных. Сохранять доступность, semantic HTML, keyboard navigation и адаптивность.
- Обязательные scripts: `dev`, `build`, `start`, `lint`, `typecheck`, `test`. Перед завершением изменений выполнять lint, typecheck, tests и production build.
- Для Docker включать `output: 'standalone'`; runtime image не должен содержать dev dependencies или секреты.
- Не смешивать frontend source с Go packages и не хранить frontend assets внутри Go backend.

Основные источники: [Project Structure](https://nextjs.org/docs/app/getting-started/project-structure), [Server and Client Components](https://nextjs.org/docs/app/getting-started/server-and-client-components), [Backend for Frontend](https://nextjs.org/docs/app/guides/backend-for-frontend), [Production Checklist](https://nextjs.org/docs/app/guides/production-checklist), [Deploying](https://nextjs.org/docs/app/getting-started/deploying).
