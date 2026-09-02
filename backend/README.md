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
