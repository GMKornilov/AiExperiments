FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api-server ./cmd/api-server

FROM alpine:3.21

RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
RUN mkdir -p /app/data && chown app:app /app/data && chmod 700 /app/data
COPY --from=build /out/api-server /usr/local/bin/api-server
COPY --chown=app:app prompts ./prompts

USER app
EXPOSE 8080
CMD ["api-server", "--config=config.yaml"]
