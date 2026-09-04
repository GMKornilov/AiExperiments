package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
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

// TemperatureService is the one-shot completion capability used by the temperature handler.
type TemperatureService interface {
	Complete(context.Context, string, float64) (string, error)
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
	var algorithmService AlgorithmService
	if len(algorithmServices) > 0 {
		algorithmService = algorithmServices[0]
	}
	return newHandler(service, algorithmService, nil)
}

// NewHandlerWithTemperature creates an API handler that also serves POST /api/temperature.
func NewHandlerWithTemperature(service ChatService, algorithmService AlgorithmService, temperatureService TemperatureService) http.Handler {
	return newHandler(service, algorithmService, temperatureService)
}

func newHandler(service ChatService, algorithmService AlgorithmService, temperatureService TemperatureService) http.Handler {
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
		case "/api/algorithms/direct", "/api/algorithms/step-by-step", "/api/algorithms/generated-prompt", "/api/algorithms/experts":
			if request.Method != http.MethodPost {
				methodNotAllowed(writer, http.MethodPost)
				return
			}
			algorithmHandler(algorithmService, algorithmMethod(request.URL.Path)).ServeHTTP(writer, request)
		case "/api/temperature":
			writer.Header().Set("Cache-Control", "no-store")
			if request.Method != http.MethodPost {
				methodNotAllowed(writer, http.MethodPost)
				return
			}
			temperatureHandler(temperatureService).ServeHTTP(writer, request)
		default:
			http.NotFound(writer, request)
		}
	})
}

type temperatureRequest struct {
	Prompt      string  `json:"prompt"`
	Temperature float64 `json:"temperature"`
}

type temperatureResponse struct {
	Answer string `json:"answer"`
}

func temperatureHandler(service TemperatureService) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := decodeTemperatureRequest(writer, request)
		if err != nil {
			writeTemperatureError(writer, temperatureErrorStatus(err))
			return
		}
		if service == nil {
			writeTemperatureError(writer, http.StatusBadGateway)
			return
		}

		answer, err := service.Complete(request.Context(), payload.Prompt, payload.Temperature)
		if err != nil || strings.TrimSpace(answer) == "" {
			writeTemperatureError(writer, http.StatusBadGateway)
			return
		}
		writeTemperatureJSON(writer, http.StatusOK, temperatureResponse{Answer: strings.TrimSpace(answer)})
	}
}

func decodeTemperatureRequest(writer http.ResponseWriter, request *http.Request) (temperatureRequest, error) {
	var result temperatureRequest
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return result, errUnsupportedMediaType
	}
	_ = http.NewResponseController(writer).SetReadDeadline(time.Now().Add(10 * time.Second))
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)

	decoder := json.NewDecoder(request.Body)
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		return result, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, errors.New("тело должно содержать ровно один JSON-объект")
	}
	if len(fields) != 2 {
		return result, errors.New("тело должно содержать только prompt и temperature")
	}
	prompt, hasPrompt := fields["prompt"]
	temperatureValue, hasTemperature := fields["temperature"]
	if !hasPrompt || !hasTemperature || strings.TrimSpace(string(temperatureValue)) == "null" || json.Unmarshal(prompt, &result.Prompt) != nil || json.Unmarshal(temperatureValue, &result.Temperature) != nil {
		return result, errors.New("некорректные поля запроса")
	}
	result.Prompt = strings.TrimSpace(result.Prompt)
	if result.Prompt == "" || utf8.RuneCountInString(result.Prompt) > maxPromptRunes {
		return result, errors.New("prompt должен содержать от 1 до 4000 символов")
	}
	if math.IsNaN(result.Temperature) || math.IsInf(result.Temperature, 0) || result.Temperature < 0 || result.Temperature > 2 {
		return result, errors.New("temperature должна быть числом от 0 до 2")
	}
	return result, nil
}

func temperatureErrorStatus(err error) int {
	if errors.Is(err, errUnsupportedMediaType) {
		return http.StatusUnsupportedMediaType
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func writeTemperatureJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, status, value)
}

func writeTemperatureError(writer http.ResponseWriter, status int) {
	message := "Проверьте prompt и температуру от 0 до 2."
	if status == http.StatusBadGateway {
		message = "Сервис временно недоступен. Повторите запрос позже."
	}
	writeTemperatureJSON(writer, status, map[string]string{"error": message})
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
