# Backend AI-бариста

```sh
cp config.example.yaml config.yaml
cp llm.example.yaml llm.yaml
# заполните api_key в llm.yaml
go run ./cmd/api-server --config=config.yaml
```

`config.yaml` содержит `addr`, `llm_config_path` и `log_text_payloads`.
`llm.yaml` содержит обязательные секции `chat` и `text`. В каждой указаны
endpoint, credential, модель, timeout и путь к своему system prompt. `chat`
создаёт ответ бариста; `text` один раз асинхронно создаёт название после первого
принятого сообщения. Backend читает оба снимка при создании диалога и хранит их
только в ОЗУ. Обычные chat API требуют `X-Session-ID`; admin lookup принимает
только точный ID диалога. Все данные исчезают при перезапуске.

В каждом endpoint необязательное `temperature` — конечное число от `0` до `2`;
если оно отсутствует, используется `1`. Значение `0` передаётся провайдеру как
явное поле JSON.
