# Backend AI-бариста

Из каталога `backend/`:

Локальные собранные binaries при необходимости можно хранить в `backend/bin/`.

```sh
cp config.example.yaml config.yaml
go run ./cmd/llm-chat --mode=free
go run ./cmd/llm-chat --mode=controlled
go run ./cmd/api-server --config=config.yaml --addr=:8080
go build ./cmd/llm-chat
go build ./cmd/api-server
go test ./...
go vet ./...
```

Алгоритмический API принимает `POST` на `/api/algorithms/direct`,
`/api/algorithms/step-by-step`, `/api/algorithms/generated-prompt` и
`/api/algorithms/experts` с JSON-снимком `{"statement":"...","language":"python"}`.
Допустимые языки: `python`, `java`, `cpp`. Тело ограничено 64 KiB, условие —
10 000 Unicode-символов; ответ LLM — 1 MiB, ответ API — должен быть ограничен
BFF до 8 MiB. Чтение входящего алгоритмического запроса ограничено 10 секундами.
CLI и AI-бариста используют `request_timeout` (по умолчанию 30 секунд).
Алгоритмы используют независимый `algorithm_request_timeout` (по умолчанию и максимум
180 секунд) для каждого LLM-вызова; метод `generated-prompt` выполняет два
последовательных вызова с отдельным полным бюджетом каждого.
Шесть алгоритмических шаблонов `algorithm-*.txt` загружаются и проверяются при старте из
`algorithms_prompts_dir` (по умолчанию `prompts`, относительно `config.yaml`); изменение
файлов применяется после перезапуска без пересборки Go-кода.
