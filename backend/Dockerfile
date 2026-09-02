FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/llm-chat ./cmd/llm-chat
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api-server ./cmd/api-server

FROM alpine:3.21

RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/llm-chat /usr/local/bin/llm-chat
COPY --from=build /out/api-server /usr/local/bin/api-server
COPY --chown=app:app prompts ./prompts
COPY --chown=app:app schemas ./schemas

USER app
EXPOSE 8080
CMD ["api-server", "--config=config.yaml", "--addr=:8080"]
