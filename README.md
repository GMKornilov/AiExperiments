# AI-бариста: CLI и Web

AI-бариста доступен как интерактивный CLI и web-интерфейс. Каждый пользовательский запрос отправляется ровно один раз в DeepSeek / OpenAI-совместимый API в режиме свободного текста или контролируемого JSON-ответа.

## Локальный запуск

1. Создайте локальный конфиг и укажите ключ:

   ```sh
   cp backend/config.example.yaml backend/config.yaml
   ```

2. Отредактируйте `config.yaml`: заполните `api_key`; при необходимости измените `base_url`, `model` и пути к prompt/schema. Отдельные пути `free_system_prompt_path` и `controlled_system_prompt_path` позволяют менять поведение режимов независимо; по умолчанию используются `prompts/barista-free-system.txt`, `prompts/barista-controlled-system.txt` и `schemas/barista-response.schema.json`.

3. Запустите в нужном режиме:

   ```sh
   cd backend && go run ./cmd/llm-chat --mode=free
   ```

   ```sh
   cd backend && go run ./cmd/llm-chat --mode=controlled
   ```

   Для сборки CLI:

   ```sh
   cd backend && go build ./cmd/llm-chat
   ```

4. Введите сообщение после `>>> `. Каждый непустой ввод соответствует ровно одному API request и выводится как `<<< <answer>`. В `free` передаются отдельные system и user messages без schema и `response_format`; ответ не валидируется. В `controlled` используется DeepSeek-compatible `response_format: {"type":"json_object"}`, controlled prompt и полная JSON Schema в system message, а ответ локально проверяется по schema и её word-limit-аннотациям. Чтобы завершить программу, передайте EOF: `Ctrl+D` в macOS/Linux.

## Локальный API и Web-интерфейс

После создания `config.yaml` запустите сервер:

```sh
cd backend && go run ./cmd/api-server --config=config.yaml --addr=:8080
```

В отдельном терминале запустите frontend:

```sh
cd frontend
npm ci
npm run dev
```

Откройте [http://localhost:3000](http://localhost:3000). Каждый submit выполняет ровно один API-вызов. API-ключ и prompts остаются только в Go API-сервере.

## Docker

Основной способ запустить весь стек — Compose:

```sh
docker compose up --build
```

Для публичного деплоя укажите внешний URL до сборки: `SITE_URL=https://example.com docker compose up --build`.

Web-интерфейс будет доступен на [http://localhost:3000](http://localhost:3000), а API останется внутренним сервисом Compose.

Для отдельного backend API-образа:

```sh
docker build -t ai-barista-api ./backend
docker run --rm -p 8080:8080 -v "$(pwd)/backend/config.yaml:/app/config.yaml:ro" ai-barista-api
```

CLI доступен в том же backend-образе:

```sh
docker run --rm -it -v "$(pwd)/backend/config.yaml:/app/config.yaml:ro" ai-barista-api llm-chat --mode=free
```

При отдельной сборке frontend используйте его Dockerfile и передайте URL API:

```sh
docker build --build-arg SITE_URL=https://example.com -t ai-barista-web -f frontend/Dockerfile frontend
docker run --rm -p 3000:3000 -e BARISTA_BACKEND_URL=http://host.docker.internal:8080 ai-barista-web
```

Backend-образ не содержит `config.yaml` или API-ключ: конфигурация всегда монтируется read-only при запуске.

`config.yaml` добавлен в `.gitignore`, поэтому ключ не попадёт в Git. Для другого OpenAI-совместимого провайдера укажите его базовый URL без конечного `/chat/completions`.

## Структура

```text
backend/             отдельный Go backend module; config и пути prompts/schemas относительны к backend cwd
backend/cmd/          CLI и API entrypoints
backend/internal/     приватные Go packages
frontend/             отдельное Next.js-приложение
backend/Dockerfile    multi-stage backend-образ CLI и API
frontend/Dockerfile   frontend-образ Next.js
docker-compose.yml    два сервиса: barista-api и barista-web
```

Каждый запрос ограничен параметром `request_timeout`. Пустое введённое сообщение не отправляется в API. Файлы prompt и schema загружаются при старте, поэтому их можно менять без изменения Go-кода.
