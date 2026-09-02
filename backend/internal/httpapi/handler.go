package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"aichallenge/week_1/task_1/internal/barista"
)

const (
	maxRequestBody = 64 << 10
	maxPromptRunes = 4000
)

// ChatService is the shared barista service used by HTTP handlers.
type ChatService interface {
	Chat(context.Context, barista.Mode, string) (string, error)
}

type chatRequest struct {
	Mode   string `json:"mode"`
	Prompt string `json:"prompt"`
}

type chatResponse struct {
	Mode barista.Mode `json:"mode"`
	Raw  string       `json:"raw"`
	Data any          `json:"data"`
}

// NewHandler creates the barista HTTP API handler.
func NewHandler(service ChatService) http.Handler {
	chat := chatHandler(service)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/chat":
			if request.Method != http.MethodPost {
				methodNotAllowed(writer, http.MethodPost)
				return
			}
			chat.ServeHTTP(writer, request)
		case "/healthz":
			if request.Method != http.MethodGet {
				methodNotAllowed(writer, http.MethodGet)
				return
			}
			healthHandler(writer, request)
		default:
			http.NotFound(writer, request)
		}
	})
}

func chatHandler(service ChatService) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		body, err := decodeChatRequest(writer, request)
		if err != nil {
			writeJSONError(writer, http.StatusBadRequest, err)
			return
		}

		answer, err := service.Chat(request.Context(), body.Mode, body.Prompt)
		if err != nil {
			writeJSONError(writer, http.StatusBadGateway, err)
			return
		}

		response := chatResponse{Mode: body.Mode, Raw: answer}
		if body.Mode == barista.ModeControlled {
			var data map[string]any
			if err := json.Unmarshal([]byte(answer), &data); err != nil {
				writeJSONError(writer, http.StatusBadGateway, fmt.Errorf("контролируемый ответ не является JSON-объектом"))
				return
			}
			response.Data = data
		}
		writeJSON(writer, http.StatusOK, response)
	}
}

func decodeChatRequest(writer http.ResponseWriter, request *http.Request) (struct {
	Mode   barista.Mode
	Prompt string
}, error) {
	var result struct {
		Mode   barista.Mode
		Prompt string
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload chatRequest
	if err := decoder.Decode(&payload); err != nil {
		return result, fmt.Errorf("некорректное JSON-тело: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, fmt.Errorf("тело должно содержать ровно один JSON-объект")
	}

	result.Mode = barista.Mode(payload.Mode)
	if result.Mode != barista.ModeFree && result.Mode != barista.ModeControlled {
		return result, fmt.Errorf("mode должен быть free или controlled")
	}
	result.Prompt = strings.TrimSpace(payload.Prompt)
	if result.Prompt == "" {
		return result, fmt.Errorf("prompt не должен быть пустым")
	}
	if utf8.RuneCountInString(result.Prompt) > maxPromptRunes {
		return result, fmt.Errorf("prompt не должен превышать %d символов", maxPromptRunes)
	}
	return result, nil
}

func healthHandler(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		log.Printf("Ошибка JSON-ответа: %v", err)
	}
}

func writeJSONError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, map[string]string{"error": err.Error()})
}

func methodNotAllowed(writer http.ResponseWriter, allowedMethod string) {
	writer.Header().Set("Allow", allowedMethod)
	writeJSONError(writer, http.StatusMethodNotAllowed, fmt.Errorf("метод не поддерживается"))
}
