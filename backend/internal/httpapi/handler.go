package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"aichallenge/week_1/task_1/internal/algorithms"
	"aichallenge/week_1/task_1/internal/barista"
)

const (
	maxRequestBody    = 64 << 10
	maxPromptRunes    = 4000
	maxStatementRunes = 10000
)

// ChatService is the shared barista service used by HTTP handlers.
type ChatService interface {
	Chat(context.Context, barista.Mode, string) (string, error)
}

// AlgorithmService is the algorithms capability used by HTTP handlers.
type AlgorithmService interface {
	Solve(context.Context, algorithms.Method, algorithms.Request) (algorithms.Result, error)
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
func NewHandler(service ChatService, algorithmServices ...AlgorithmService) http.Handler {
	chat := chatHandler(service)
	var algorithmService AlgorithmService
	if len(algorithmServices) > 0 {
		algorithmService = algorithmServices[0]
	}
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
		case "/api/algorithms/direct", "/api/algorithms/step-by-step", "/api/algorithms/generated-prompt", "/api/algorithms/experts":
			if request.Method != http.MethodPost {
				methodNotAllowed(writer, http.MethodPost)
				return
			}
			algorithmHandler(algorithmService, algorithmMethod(request.URL.Path)).ServeHTTP(writer, request)
		default:
			http.NotFound(writer, request)
		}
	})
}

type algorithmRequest struct {
	Statement string              `json:"statement"`
	Language  algorithms.Language `json:"language"`
}

type algorithmError struct {
	Code    algorithms.ErrorKind `json:"code"`
	Message string               `json:"message"`
}

type algorithmResponse struct {
	Method algorithms.Method      `json:"method"`
	Status string                 `json:"status"`
	Answer string                 `json:"answer"`
	Trace  []algorithms.TraceStep `json:"trace"`
	Error  *algorithmError        `json:"error,omitempty"`
}

func algorithmHandler(service AlgorithmService, method algorithms.Method) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		body, err := decodeAlgorithmRequest(writer, request)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errUnsupportedMediaType) {
				status = http.StatusUnsupportedMediaType
			}
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				status = http.StatusRequestEntityTooLarge
			}
			writeAlgorithmError(writer, status, method, nil, algorithms.ErrorInvalidRequest)
			return
		}
		if service == nil {
			writeAlgorithmError(writer, http.StatusBadGateway, method, nil, algorithms.ErrorUnavailable)
			return
		}

		result, err := service.Solve(request.Context(), method, body)
		if err != nil {
			kind := algorithmErrorKind(err)
			writeAlgorithmError(writer, algorithmStatus(kind), method, result.Trace, kind)
			return
		}
		writeAlgorithmJSON(writer, http.StatusOK, algorithmResponse{
			Method: result.Method,
			Status: "success",
			Answer: result.Answer,
			Trace:  result.Trace,
		})
	}
}

var errUnsupportedMediaType = errors.New("unsupported media type")

func decodeAlgorithmRequest(writer http.ResponseWriter, request *http.Request) (algorithms.Request, error) {
	var result algorithms.Request
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return result, errUnsupportedMediaType
	}
	_ = http.NewResponseController(writer).SetReadDeadline(time.Now().Add(10 * time.Second))
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		return result, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, err
	}
	if len(fields) != 2 {
		return result, errors.New("request must contain exactly statement and language")
	}
	statement, hasStatement := fields["statement"]
	language, hasLanguage := fields["language"]
	if !hasStatement || !hasLanguage {
		return result, errors.New("request must contain exact field names")
	}
	var payload algorithmRequest
	if err := json.Unmarshal(statement, &payload.Statement); err != nil {
		return result, err
	}
	if err := json.Unmarshal(language, &payload.Language); err != nil {
		return result, err
	}
	if strings.TrimSpace(payload.Statement) == "" {
		return result, errors.New("statement must not be empty")
	}
	if utf8.RuneCountInString(payload.Statement) > maxStatementRunes {
		return result, errors.New("statement is too long")
	}
	switch payload.Language {
	case algorithms.LanguagePython, algorithms.LanguageJava, algorithms.LanguageCPP:
	default:
		return result, errors.New("unsupported language")
	}
	return algorithms.Request{Statement: payload.Statement, Language: payload.Language}, nil
}

func algorithmMethod(path string) algorithms.Method {
	switch path {
	case "/api/algorithms/direct":
		return algorithms.MethodDirect
	case "/api/algorithms/step-by-step":
		return algorithms.MethodStepByStep
	case "/api/algorithms/generated-prompt":
		return algorithms.MethodGeneratedPrompt
	default:
		return algorithms.MethodExperts
	}
}

func algorithmErrorKind(err error) algorithms.ErrorKind {
	var algorithmErr *algorithms.Error
	if errors.As(err, &algorithmErr) {
		return algorithmErr.Kind
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return algorithms.ErrorTimeout
	}
	return algorithms.ErrorUnavailable
}

func algorithmStatus(kind algorithms.ErrorKind) int {
	switch kind {
	case algorithms.ErrorInvalidRequest:
		return http.StatusBadRequest
	case algorithms.ErrorTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}

func writeAlgorithmError(writer http.ResponseWriter, status int, method algorithms.Method, trace []algorithms.TraceStep, kind algorithms.ErrorKind) {
	if trace == nil {
		trace = []algorithms.TraceStep{}
	}
	writeAlgorithmJSON(writer, status, algorithmResponse{
		Method: method,
		Status: "error",
		Answer: "",
		Trace:  trace,
		Error:  &algorithmError{Code: kind, Message: algorithmErrorMessage(kind)},
	})
}

func writeAlgorithmJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, status, value)
}

func algorithmErrorMessage(kind algorithms.ErrorKind) string {
	switch kind {
	case algorithms.ErrorInvalidRequest:
		return "Проверьте заполнение формы и повторите запуск."
	case algorithms.ErrorTimeout:
		return "Время ожидания истекло. Повторите запуск."
	case algorithms.ErrorInvalidResponse:
		return "Ответ сервиса нельзя прочитать. Повторите запуск."
	default:
		return "Сервис недоступен. Повторите запуск позже."
	}
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
