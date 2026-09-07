# Backend AI-бариста

```sh
cp config.example.yaml config.yaml
cp llm.example.yaml llm.yaml
# заполните api_key в llm.yaml
go run ./cmd/api-server --config=config.yaml
```

`config.yaml` содержит `addr`, `llm_config_path` и `log_text_payloads`.
`llm.yaml` содержит endpoint, credential, модель, timeout и путь к system prompt.
Backend читает LLM config и prompt при создании диалога, сохраняя снимок только
в ОЗУ. Обычные chat API требуют `X-Session-ID`; admin lookup принимает только
точный ID диалога. Все данные исчезают при перезапуске.
