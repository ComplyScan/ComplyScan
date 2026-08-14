package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	openAIResponsesURL     = "https://api.openai.com/v1/responses"
	anthropicMessagesURL   = "https://api.anthropic.com/v1/messages"
	geminiInteractionsURL  = "https://generativelanguage.googleapis.com/v1beta/interactions"
	maxRemoteResponseBytes = 2 << 20
	maxRemoteOutputTokens  = 4096
)

var (
	remoteTokenLimitPattern = regexp.MustCompile(`(?i)limit\s+([0-9]+),\s*requested\s+([0-9]+)`)
	remoteRetryDelayPattern = regexp.MustCompile(`(?i)try again in\s+([0-9]+(?:\.[0-9]+)?)\s*(ms|s)`)
)

// RemoteRateLimitError retains the structured parts of an HTTP 429 response
// needed by repository analysis to distinguish an individually oversized
// request from a temporary rolling rate limit.
type RemoteRateLimitError struct {
	Provider        string
	Message         string
	LimitTokens     int
	RequestedTokens int
	RetryAfter      time.Duration
	RequestTooLarge bool
}

func (value *RemoteRateLimitError) Error() string {
	if value.Message != "" {
		return fmt.Sprintf("%s review failed with HTTP 429: %s", value.Provider, value.Message)
	}
	return fmt.Sprintf("%s review failed with HTTP 429", value.Provider)
}

// AsRemoteRateLimitError unwraps a provider error without requiring callers to
// parse user-facing error strings.
func AsRemoteRateLimitError(err error) (*RemoteRateLimitError, bool) {
	var value *RemoteRateLimitError
	if !errors.As(err, &value) {
		return nil, false
	}
	return value, true
}

// RemoteOptions contains a credential value only in process memory. Callers
// must resolve it from an environment variable and must never persist it.
type RemoteOptions struct {
	APIKey      string
	BaseURL     string
	Model       string
	Timeout     time.Duration
	MaxFindings int
	HTTPClient  *http.Client
}

func NewOpenAI(options RemoteOptions) (*OllamaProvider, error) {
	return newRemoteProvider(OpenAI, "OpenAI", options, openAICompletion)
}

func NewAnthropic(options RemoteOptions) (*OllamaProvider, error) {
	return newRemoteProvider(Anthropic, "Anthropic", options, anthropicCompletion)
}

func NewGemini(options RemoteOptions) (*OllamaProvider, error) {
	return newRemoteProvider(Gemini, "Gemini", options, geminiCompletion)
}

func NewOpenAICompatible(kind Kind, label string, options RemoteOptions) (*OllamaProvider, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil, errors.New("create OpenAI-compatible provider: label must not be empty")
	}
	baseURL, err := validateCompatibleBaseURL(options.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("create %s provider: %w", label, err)
	}
	return newRemoteProvider(kind, label, options, openAICompatibleCompletion(baseURL, label))
}

type remoteCompletionFactory func(*http.Client, string, string) func(context.Context, ollamaChatRequest) (ollamaChatResponse, error)

func validateCompatibleBaseURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		return "", errors.New("base URL must be an absolute HTTPS URL")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return "", errors.New("base URL must not contain credentials, query parameters, or a fragment")
	}
	return value, nil
}

func newRemoteProvider(kind Kind, label string, options RemoteOptions, factory remoteCompletionFactory) (*OllamaProvider, error) {
	if strings.TrimSpace(options.APIKey) == "" {
		return nil, fmt.Errorf("create %s provider: API key is not available", label)
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		return nil, fmt.Errorf("create %s provider: model must not be empty", label)
	}
	if options.Timeout <= 0 {
		return nil, fmt.Errorf("create %s provider: timeout must be greater than zero", label)
	}
	if options.MaxFindings <= 0 || options.MaxFindings > 100 {
		return nil, fmt.Errorf("create %s provider: max findings must be between 1 and 100", label)
	}
	client := options.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		client = &http.Client{
			Transport: transport,
			Timeout:   options.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &OllamaProvider{
		kind: kind, label: label, model: model, maxFindings: options.MaxFindings,
		client: client, completion: factory(client, strings.TrimSpace(options.APIKey), model),
	}, nil
}

func openAICompletion(client *http.Client, apiKey, model string) func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
	return func(ctx context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
		messages, err := remoteMessages(request.Messages)
		if err != nil {
			return ollamaChatResponse{}, err
		}
		body := map[string]any{
			"model":             model,
			"input":             messages,
			"store":             false,
			"max_output_tokens": remoteOutputTokenLimit(request),
			"text": map[string]any{"format": map[string]any{
				"type": "json_schema", "name": "complyscan_output", "strict": true, "schema": request.Format,
			}},
		}
		var payload struct {
			Status string `json:"status"`
			Output []struct {
				Type    string `json:"type"`
				Content []struct {
					Type    string `json:"type"`
					Text    string `json:"text"`
					Refusal string `json:"refusal"`
				} `json:"content"`
			} `json:"output"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		started := time.Now()
		if err := postRemoteJSON(ctx, client, "OpenAI", openAIResponsesURL, apiKey, "Authorization", "Bearer ", body, &payload, nil); err != nil {
			return ollamaChatResponse{}, err
		}
		if payload.Status != "completed" {
			return ollamaChatResponse{}, fmt.Errorf("OpenAI review returned status %q", cleanReviewText(payload.Status, 100))
		}
		content := ""
		for _, output := range payload.Output {
			for _, block := range output.Content {
				if block.Type == "refusal" && strings.TrimSpace(block.Refusal) != "" {
					return ollamaChatResponse{}, errors.New("OpenAI declined the structured review request")
				}
				if block.Type == "output_text" {
					content += block.Text
				}
			}
		}
		return remoteResponse(content, payload.Usage.InputTokens, payload.Usage.OutputTokens, time.Since(started), "OpenAI")
	}
}

func anthropicCompletion(client *http.Client, apiKey, model string) func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
	return func(ctx context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
		system, user, err := remotePromptPair(request.Messages)
		if err != nil {
			return ollamaChatResponse{}, err
		}
		body := map[string]any{
			"model": model, "max_tokens": remoteOutputTokenLimit(request), "system": system,
			"messages":      []map[string]string{{"role": "user", "content": user}},
			"output_config": map[string]any{"format": map[string]any{"type": "json_schema", "schema": request.Format}},
		}
		var payload struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		headers := map[string]string{"anthropic-version": "2023-06-01"}
		started := time.Now()
		if err := postRemoteJSON(ctx, client, "Anthropic", anthropicMessagesURL, apiKey, "x-api-key", "", body, &payload, headers); err != nil {
			return ollamaChatResponse{}, err
		}
		if payload.StopReason != "end_turn" && payload.StopReason != "stop_sequence" {
			return ollamaChatResponse{}, fmt.Errorf("Anthropic review stopped with reason %q", cleanReviewText(payload.StopReason, 100))
		}
		content := ""
		for _, block := range payload.Content {
			if block.Type == "text" {
				content += block.Text
			}
		}
		return remoteResponse(content, payload.Usage.InputTokens, payload.Usage.OutputTokens, time.Since(started), "Anthropic")
	}
}

func geminiCompletion(client *http.Client, apiKey, model string) func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
	return func(ctx context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
		system, user, err := remotePromptPair(request.Messages)
		if err != nil {
			return ollamaChatResponse{}, err
		}
		body := map[string]any{
			"model": model, "store": false,
			"input":           system + "\n\nUser task and untrusted repository evidence follow.\n\n" + user,
			"response_format": map[string]any{"type": "text", "mime_type": "application/json", "schema": request.Format},
		}
		var payload struct {
			Status string `json:"status"`
			Steps  []struct {
				Type    string `json:"type"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"steps"`
			Usage struct {
				InputTokens  int `json:"total_input_tokens"`
				OutputTokens int `json:"total_output_tokens"`
			} `json:"usage"`
		}
		started := time.Now()
		if err := postRemoteJSON(ctx, client, "Gemini", geminiInteractionsURL, apiKey, "x-goog-api-key", "", body, &payload, nil); err != nil {
			return ollamaChatResponse{}, err
		}
		if payload.Status != "completed" {
			return ollamaChatResponse{}, fmt.Errorf("Gemini review returned status %q", cleanReviewText(payload.Status, 100))
		}
		content := ""
		for _, step := range payload.Steps {
			if step.Type != "model_output" {
				continue
			}
			for _, block := range step.Content {
				if block.Type == "text" {
					content += block.Text
				}
			}
		}
		return remoteResponse(content, payload.Usage.InputTokens, payload.Usage.OutputTokens, time.Since(started), "Gemini")
	}
}

func openAICompatibleCompletion(baseURL, label string) remoteCompletionFactory {
	return func(client *http.Client, apiKey, model string) func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
		return func(ctx context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			messages, err := remoteMessages(request.Messages)
			if err != nil {
				return ollamaChatResponse{}, err
			}
			body := map[string]any{
				"model": model, "messages": messages, "max_tokens": remoteOutputTokenLimit(request),
				"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{
					"name": "complyscan_output", "strict": true, "schema": request.Format,
				}},
			}
			var payload struct {
				Choices []struct {
					FinishReason string `json:"finish_reason"`
					Message      struct {
						Content string `json:"content"`
						Refusal string `json:"refusal"`
					} `json:"message"`
				} `json:"choices"`
				Usage struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				} `json:"usage"`
			}
			started := time.Now()
			if err := postRemoteJSON(ctx, client, label, baseURL+"/chat/completions", apiKey, "Authorization", "Bearer ", body, &payload, nil); err != nil {
				return ollamaChatResponse{}, err
			}
			if len(payload.Choices) != 1 {
				return ollamaChatResponse{}, fmt.Errorf("%s review returned %d choices; expected one", label, len(payload.Choices))
			}
			choice := payload.Choices[0]
			if strings.TrimSpace(choice.Message.Refusal) != "" {
				return ollamaChatResponse{}, fmt.Errorf("%s declined the structured review request", label)
			}
			if choice.FinishReason != "stop" {
				return ollamaChatResponse{}, fmt.Errorf("%s review stopped with reason %q", label, cleanReviewText(choice.FinishReason, 100))
			}
			return remoteResponse(choice.Message.Content, payload.Usage.PromptTokens, payload.Usage.CompletionTokens, time.Since(started), label)
		}
	}
}

func remoteOutputTokenLimit(request ollamaChatRequest) int {
	if request.MaxOutputTokens > 0 && request.MaxOutputTokens <= 16_384 {
		return request.MaxOutputTokens
	}
	return maxRemoteOutputTokens
}

func remoteMessages(messages []ollamaMessage) ([]map[string]string, error) {
	if len(messages) == 0 {
		return nil, errors.New("remote review request contains no messages")
	}
	result := make([]map[string]string, 0, len(messages))
	for _, message := range messages {
		if message.Role != "system" && message.Role != "user" {
			return nil, fmt.Errorf("remote review request contains unsupported role %q", message.Role)
		}
		result = append(result, map[string]string{"role": message.Role, "content": message.Content})
	}
	return result, nil
}

func remotePromptPair(messages []ollamaMessage) (string, string, error) {
	system, user := "", ""
	for _, message := range messages {
		switch message.Role {
		case "system":
			system += message.Content
		case "user":
			user += message.Content
		default:
			return "", "", fmt.Errorf("remote review request contains unsupported role %q", message.Role)
		}
	}
	if strings.TrimSpace(system) == "" || strings.TrimSpace(user) == "" {
		return "", "", errors.New("remote review request requires system and user content")
	}
	return system, user, nil
}

func postRemoteJSON(ctx context.Context, client *http.Client, label, endpoint, apiKey, keyHeader, keyPrefix string, body any, target any, extraHeaders map[string]string) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", label, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create %s request: %w", label, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "ComplyScan")
	request.Header.Set(keyHeader, keyPrefix+apiKey)
	for name, value := range extraHeaders {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request %s review: %w", label, err)
	}
	defer response.Body.Close()
	responseBody, err := readLimited(response.Body, maxRemoteResponseBytes)
	if err != nil {
		return fmt.Errorf("read %s response: %w", label, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return remoteStatusError(label, response.StatusCode, responseBody, response.Header.Get("Retry-After"))
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return fmt.Errorf("decode %s response: %w", label, err)
	}
	return nil
}

func remoteStatusError(label string, status int, body []byte, retryAfterHeader string) error {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	message := ""
	if json.Unmarshal(body, &payload) == nil {
		message = cleanReviewText(payload.Error.Message, maxReviewMessageChars)
	}
	if status == http.StatusTooManyRequests {
		limit, requested := remoteTokenLimit(message)
		return &RemoteRateLimitError{
			Provider: label, Message: message, LimitTokens: limit, RequestedTokens: requested,
			RetryAfter:      remoteRetryAfter(retryAfterHeader, message),
			RequestTooLarge: strings.Contains(strings.ToLower(message), "request too large") || limit > 0 && requested > limit,
		}
	}
	if message != "" {
		return fmt.Errorf("%s review failed with HTTP %d: %s", label, status, message)
	}
	return fmt.Errorf("%s review failed with HTTP %d", label, status)
}

func remoteTokenLimit(message string) (int, int) {
	match := remoteTokenLimitPattern.FindStringSubmatch(message)
	if len(match) != 3 {
		return 0, 0
	}
	limit, _ := strconv.Atoi(match[1])
	requested, _ := strconv.Atoi(match[2])
	return limit, requested
}

func remoteRetryAfter(header, message string) time.Duration {
	if seconds, err := strconv.ParseFloat(strings.TrimSpace(header), 64); err == nil && seconds > 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if timestamp, err := http.ParseTime(strings.TrimSpace(header)); err == nil {
		if delay := time.Until(timestamp); delay > 0 {
			return delay
		}
	}
	match := remoteRetryDelayPattern.FindStringSubmatch(message)
	if len(match) != 3 {
		return 0
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil || value <= 0 {
		return 0
	}
	unit := time.Second
	if strings.EqualFold(match[2], "ms") {
		unit = time.Millisecond
	}
	return time.Duration(value * float64(unit))
}

func remoteResponse(content string, inputTokens, outputTokens int, duration time.Duration, label string) (ollamaChatResponse, error) {
	if strings.TrimSpace(content) == "" {
		return ollamaChatResponse{}, fmt.Errorf("%s review returned an empty structured response", label)
	}
	response := ollamaChatResponse{Done: true, PromptEvalCount: inputTokens, EvalCount: outputTokens, TotalDuration: duration.Nanoseconds()}
	response.Message.Content = content
	return response, nil
}
