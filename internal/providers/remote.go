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

// OpenAIMaxOutputTokens is the documented GPT-5.6 output ceiling used to
// prevent an account-level token allowance from producing an invalid request.
const OpenAIMaxOutputTokens = 128_000

var (
	remoteTokenLimitPattern = regexp.MustCompile(`(?i)limit\s+([0-9]+),\s*requested\s+([0-9]+)`)
	remoteRetryDelayPattern = regexp.MustCompile(`(?i)(?:try again|retry) in\s+([0-9]+(?:\.[0-9]+)?)\s*(ms|s)`)
)

// RemoteRateLimitError retains the structured parts of a provider capacity or
// request-size response. Providers report oversized context as 400, 413, or
// 429, while temporary rolling quotas normally use 429.
type RemoteRateLimitError struct {
	Provider        string
	StatusCode      int
	Message         string
	Code            string
	LimitTokens     int
	RequestedTokens int
	RetryAfter      time.Duration
	RequestTooLarge bool
	Permanent       bool
	RateLimits      RateLimitSnapshot
}

func (value *RemoteRateLimitError) Error() string {
	status := value.StatusCode
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		if value.Message != "" {
			return fmt.Sprintf("%s review stopped: %s", value.Provider, value.Message)
		}
		return fmt.Sprintf("%s review stopped before producing a complete result", value.Provider)
	}
	if status == 0 {
		status = http.StatusTooManyRequests
	}
	if value.Message != "" {
		return fmt.Sprintf("%s review failed with HTTP %d: %s", value.Provider, status, value.Message)
	}
	return fmt.Sprintf("%s review failed with HTTP %d", value.Provider, status)
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

// RemoteTransientError describes a provider or transport-adjacent HTTP
// failure that can be retried without changing the review request. Callers
// still bound attempts and elapsed time so a provider outage cannot hang a
// scan indefinitely.
type RemoteTransientError struct {
	Provider   string
	StatusCode int
	Message    string
	RetryAfter time.Duration
	RateLimits RateLimitSnapshot
}

func (value *RemoteTransientError) Error() string {
	if value.StatusCode == 0 {
		if value.Message != "" {
			return fmt.Sprintf("%s review encountered a temporary transport failure: %s", value.Provider, value.Message)
		}
		return fmt.Sprintf("%s review encountered a temporary transport failure", value.Provider)
	}
	if value.Message != "" {
		return fmt.Sprintf("%s review failed with HTTP %d: %s", value.Provider, value.StatusCode, value.Message)
	}
	return fmt.Sprintf("%s review failed with HTTP %d", value.Provider, value.StatusCode)
}

func AsRemoteTransientError(err error) (*RemoteTransientError, bool) {
	var value *RemoteTransientError
	if !errors.As(err, &value) {
		return nil, false
	}
	return value, true
}

// RemoteIncompleteError preserves a successful HTTP response that the
// provider could not complete within its output allowance. Repository
// analysis uses the structured reason and usage to choose a smaller slice.
type RemoteIncompleteError struct {
	Provider        string
	Status          string
	Reason          string
	InputTokens     int
	OutputTokens    int
	ReasoningTokens int
	TokenLimit      int
	RateLimits      RateLimitSnapshot
}

func (value *RemoteIncompleteError) Error() string {
	status := cleanReviewText(value.Status, 100)
	if value.Reason != "" {
		return fmt.Sprintf("%s review returned status %q (reason: %s; input tokens: %d; output tokens: %d, including %d reasoning tokens)", value.Provider, status, cleanReviewText(value.Reason, 100), value.InputTokens, value.OutputTokens, value.ReasoningTokens)
	}
	return fmt.Sprintf("%s review returned status %q without incomplete_details.reason (input tokens: %d; output tokens: %d, including %d reasoning tokens)", value.Provider, status, value.InputTokens, value.OutputTokens, value.ReasoningTokens)
}

// AsRemoteIncompleteError unwraps a provider response that ended before a
// complete structured result was available.
func AsRemoteIncompleteError(err error) (*RemoteIncompleteError, bool) {
	var value *RemoteIncompleteError
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
		textConfig := map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "complyscan_output", "strict": true, "schema": request.Format,
		}}
		if request.TextVerbosity != "" && openAISupportsTextVerbosity(model) {
			textConfig["verbosity"] = request.TextVerbosity
		}
		body := map[string]any{
			"model":             model,
			"input":             messages,
			"store":             false,
			"max_output_tokens": remoteOutputTokenLimit(request, OpenAIMaxOutputTokens),
			"text":              textConfig,
		}
		if openAISupportsTemperature(model) {
			body["temperature"] = 0
		}
		if request.ReasoningEffort != "" && openAISupportsReasoningEffort(model) {
			body["reasoning"] = map[string]any{"effort": request.ReasoningEffort}
		}
		var payload struct {
			Status            string `json:"status"`
			IncompleteDetails struct {
				Reason string `json:"reason"`
			} `json:"incomplete_details"`
			Output []struct {
				Type    string `json:"type"`
				Content []struct {
					Type    string `json:"type"`
					Text    string `json:"text"`
					Refusal string `json:"refusal"`
				} `json:"content"`
			} `json:"output"`
			Usage struct {
				InputTokens   int `json:"input_tokens"`
				OutputTokens  int `json:"output_tokens"`
				OutputDetails struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"output_tokens_details"`
			} `json:"usage"`
		}
		started := time.Now()
		var responseHeaders http.Header
		if err := postRemoteJSON(ctx, client, "OpenAI", openAIResponsesURL, apiKey, "Authorization", "Bearer ", body, &payload, &responseHeaders, nil); err != nil {
			return ollamaChatResponse{RateLimits: remoteRateLimitSnapshot(responseHeaders)}, err
		}
		rateLimits := remoteRateLimitSnapshot(responseHeaders)
		accounting := remoteUsageResponse(payload.Usage.InputTokens, payload.Usage.OutputTokens, payload.Usage.OutputDetails.ReasoningTokens, time.Since(started), rateLimits)
		if payload.Status != "completed" {
			return accounting, &RemoteIncompleteError{
				Provider: "OpenAI", Status: payload.Status, Reason: payload.IncompleteDetails.Reason,
				InputTokens: payload.Usage.InputTokens, OutputTokens: payload.Usage.OutputTokens,
				ReasoningTokens: payload.Usage.OutputDetails.ReasoningTokens,
				RateLimits:      rateLimits,
			}
		}
		content := ""
		for _, output := range payload.Output {
			for _, block := range output.Content {
				if block.Type == "refusal" && strings.TrimSpace(block.Refusal) != "" {
					return accounting, errors.New("OpenAI declined the structured review request")
				}
				if block.Type == "output_text" {
					content += block.Text
				}
			}
		}
		response, err := remoteResponse(content, payload.Usage.InputTokens, payload.Usage.OutputTokens, time.Since(started), "OpenAI")
		response.ReasoningCount = payload.Usage.OutputDetails.ReasoningTokens
		response.RateLimits = rateLimits
		return response, err
	}
}

func anthropicCompletion(client *http.Client, apiKey, model string) func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
	return func(ctx context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
		system, user, err := remotePromptPair(request.Messages)
		if err != nil {
			return ollamaChatResponse{}, err
		}
		outputLimit := remoteOutputTokenLimit(request, 16_384)
		outputConfig := map[string]any{"format": map[string]any{"type": "json_schema", "schema": anthropicOutputSchema(request.Format)}}
		if request.ReasoningEffort != "" && anthropicSupportsEffort(model) {
			outputConfig["effort"] = request.ReasoningEffort
		}
		body := map[string]any{
			"model": model, "max_tokens": outputLimit, "system": system,
			"messages":      []map[string]string{{"role": "user", "content": user}},
			"output_config": outputConfig,
		}
		var payload struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Usage struct {
				InputTokens   int `json:"input_tokens"`
				OutputTokens  int `json:"output_tokens"`
				OutputDetails struct {
					ThinkingTokens int `json:"thinking_tokens"`
				} `json:"output_tokens_details"`
			} `json:"usage"`
		}
		headers := map[string]string{"anthropic-version": "2023-06-01"}
		started := time.Now()
		var responseHeaders http.Header
		if err := postRemoteJSON(ctx, client, "Anthropic", anthropicMessagesURL, apiKey, "x-api-key", "", body, &payload, &responseHeaders, headers); err != nil {
			return ollamaChatResponse{RateLimits: remoteRateLimitSnapshot(responseHeaders)}, err
		}
		rateLimits := remoteRateLimitSnapshot(responseHeaders)
		accounting := remoteUsageResponse(payload.Usage.InputTokens, payload.Usage.OutputTokens, payload.Usage.OutputDetails.ThinkingTokens, time.Since(started), rateLimits)
		if payload.StopReason == "max_tokens" {
			return accounting, &RemoteIncompleteError{
				Provider: "Anthropic", Status: "incomplete", Reason: "max_output_tokens",
				InputTokens: payload.Usage.InputTokens, OutputTokens: payload.Usage.OutputTokens,
				ReasoningTokens: payload.Usage.OutputDetails.ThinkingTokens, RateLimits: rateLimits,
			}
		}
		if payload.StopReason == "model_context_window_exceeded" {
			return accounting, &RemoteRateLimitError{
				Provider: "Anthropic", StatusCode: http.StatusOK,
				Message: "model context window exceeded", Code: payload.StopReason,
				RequestTooLarge: true, RateLimits: rateLimits,
			}
		}
		if payload.StopReason != "end_turn" && payload.StopReason != "stop_sequence" {
			return accounting, fmt.Errorf("Anthropic review stopped with reason %q", cleanReviewText(payload.StopReason, 100))
		}
		content := ""
		for _, block := range payload.Content {
			if block.Type == "text" {
				content += block.Text
			}
		}
		response, err := remoteResponse(content, payload.Usage.InputTokens, payload.Usage.OutputTokens, time.Since(started), "Anthropic")
		response.ReasoningCount = payload.Usage.OutputDetails.ThinkingTokens
		response.RateLimits = rateLimits
		return response, err
	}
}

func geminiCompletion(client *http.Client, apiKey, model string) func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
	return func(ctx context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
		system, user, err := remotePromptPair(request.Messages)
		if err != nil {
			return ollamaChatResponse{}, err
		}
		outputLimit := remoteOutputTokenLimit(request, 16_384)
		generationConfig := map[string]any{"max_output_tokens": outputLimit, "seed": 0}
		if thinkingLevel := geminiThinkingLevel(request.ReasoningEffort); thinkingLevel != "" {
			generationConfig["thinking_level"] = thinkingLevel
		}
		body := map[string]any{
			"model": model, "store": false,
			"system_instruction": system,
			"input":              user,
			"response_format":    map[string]any{"type": "text", "mime_type": "application/json", "schema": geminiOutputSchema(request.Format)},
			"generation_config":  generationConfig,
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
				InputTokens   int `json:"total_input_tokens"`
				OutputTokens  int `json:"total_output_tokens"`
				ThoughtTokens int `json:"total_thought_tokens"`
			} `json:"usage"`
		}
		started := time.Now()
		var responseHeaders http.Header
		if err := postRemoteJSON(ctx, client, "Gemini", geminiInteractionsURL, apiKey, "x-goog-api-key", "", body, &payload, &responseHeaders, nil); err != nil {
			return ollamaChatResponse{RateLimits: remoteRateLimitSnapshot(responseHeaders)}, err
		}
		rateLimits := remoteRateLimitSnapshot(responseHeaders)
		accounting := remoteUsageResponse(payload.Usage.InputTokens, payload.Usage.OutputTokens, payload.Usage.ThoughtTokens, time.Since(started), rateLimits)
		status := strings.ToLower(strings.TrimSpace(payload.Status))
		if status == "incomplete" || status == "budget_exceeded" {
			return accounting, &RemoteIncompleteError{
				Provider: "Gemini", Status: payload.Status, Reason: "max_output_tokens",
				InputTokens: payload.Usage.InputTokens, OutputTokens: payload.Usage.OutputTokens,
				ReasoningTokens: payload.Usage.ThoughtTokens, RateLimits: rateLimits,
			}
		}
		if payload.Status != "completed" {
			return accounting, fmt.Errorf("Gemini review returned status %q", cleanReviewText(payload.Status, 100))
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
		response, err := remoteResponse(content, payload.Usage.InputTokens, payload.Usage.OutputTokens, time.Since(started), "Gemini")
		response.ReasoningCount = payload.Usage.ThoughtTokens
		response.RateLimits = rateLimits
		return response, err
	}
}

func openAICompatibleCompletion(baseURL, label string) remoteCompletionFactory {
	return func(client *http.Client, apiKey, model string) func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
		return func(ctx context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			messages, err := remoteMessages(request.Messages)
			if err != nil {
				return ollamaChatResponse{}, err
			}
			outputLimit := remoteOutputTokenLimit(request, 16_384)
			body := map[string]any{
				"model": model, "messages": messages, "max_tokens": outputLimit,
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
					PromptTokens      int `json:"prompt_tokens"`
					CompletionTokens  int `json:"completion_tokens"`
					CompletionDetails struct {
						ReasoningTokens int `json:"reasoning_tokens"`
					} `json:"completion_tokens_details"`
				} `json:"usage"`
			}
			started := time.Now()
			var responseHeaders http.Header
			if err := postRemoteJSON(ctx, client, label, baseURL+"/chat/completions", apiKey, "Authorization", "Bearer ", body, &payload, &responseHeaders, nil); err != nil {
				return ollamaChatResponse{RateLimits: remoteRateLimitSnapshot(responseHeaders)}, err
			}
			rateLimits := remoteRateLimitSnapshot(responseHeaders)
			reasoningTokens := payload.Usage.CompletionDetails.ReasoningTokens
			accounting := remoteUsageResponse(payload.Usage.PromptTokens, payload.Usage.CompletionTokens, reasoningTokens, time.Since(started), rateLimits)
			if len(payload.Choices) != 1 {
				return accounting, fmt.Errorf("%s review returned %d choices; expected one", label, len(payload.Choices))
			}
			choice := payload.Choices[0]
			if strings.TrimSpace(choice.Message.Refusal) != "" {
				return accounting, fmt.Errorf("%s declined the structured review request", label)
			}
			if choice.FinishReason == "length" {
				return accounting, &RemoteIncompleteError{
					Provider: label, Status: "incomplete", Reason: "max_output_tokens",
					InputTokens: payload.Usage.PromptTokens, OutputTokens: payload.Usage.CompletionTokens,
					ReasoningTokens: reasoningTokens, RateLimits: rateLimits,
				}
			}
			if choice.FinishReason != "stop" {
				return accounting, fmt.Errorf("%s review stopped with reason %q", label, cleanReviewText(choice.FinishReason, 100))
			}
			response, err := remoteResponse(choice.Message.Content, payload.Usage.PromptTokens, payload.Usage.CompletionTokens, time.Since(started), label)
			response.ReasoningCount = reasoningTokens
			response.RateLimits = rateLimits
			return response, err
		}
	}
}

func remoteOutputTokenLimit(request ollamaChatRequest, maximum int) int {
	if request.MaxOutputTokens > 0 {
		if maximum > 0 && request.MaxOutputTokens > maximum {
			return maximum
		}
		return request.MaxOutputTokens
	}
	if maximum > 0 && maxRemoteOutputTokens > maximum {
		return maximum
	}
	return maxRemoteOutputTokens
}

// Optional Responses API tuning parameters are deliberately capability-gated.
// Structured output itself is supported by a wider model set, including GPT-4o
// and GPT-4.1, but sending an unsupported reasoning or verbosity field turns an
// otherwise valid review into a permanent HTTP 400 response.
func openAISupportsReasoningEffort(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(model, "-pro") || strings.Contains(model, ".pro") || strings.Contains(model, "-chat") {
		return false
	}
	return openAIModelFamily(model, "gpt-5") || openAIModelFamily(model, "o1") ||
		openAIModelFamily(model, "o3") || openAIModelFamily(model, "o4")
}

func openAISupportsTextVerbosity(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(model, "-pro") || strings.Contains(model, ".pro") || strings.Contains(model, "-chat") || strings.Contains(model, "-codex") {
		return false
	}
	return openAIModelFamily(model, "gpt-5")
}

func openAISupportsTemperature(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return openAIModelFamily(model, "gpt-5.6")
}

func anthropicSupportsEffort(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, family := range []string{"claude-fable-5", "claude-opus-5", "claude-sonnet-5", "claude-mythos-5"} {
		if model == family || strings.HasPrefix(model, family+"-") {
			return true
		}
	}
	return false
}

func geminiThinkingLevel(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "none", "minimal":
		return "minimal"
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high", "xhigh", "max":
		return "high"
	default:
		return ""
	}
}

func openAIModelFamily(model, family string) bool {
	return model == family || strings.HasPrefix(model, family+"-") || strings.HasPrefix(model, family+".")
}

func positiveHeaderInt(headers http.Header, name string) int {
	value, err := strconv.Atoi(strings.TrimSpace(headers.Get(name)))
	if err != nil || value <= 0 {
		return 0
	}
	return value
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

func postRemoteJSON(ctx context.Context, client *http.Client, label, endpoint, apiKey, keyHeader, keyPrefix string, body any, target any, responseHeaders *http.Header, extraHeaders map[string]string) error {
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
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &RemoteTransientError{
			Provider: label,
			Message:  cleanReviewText(err.Error(), maxReviewMessageChars),
		}
	}
	defer response.Body.Close()
	if responseHeaders != nil {
		*responseHeaders = response.Header.Clone()
	}
	responseBody, err := readLimited(response.Body, maxRemoteResponseBytes)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if strings.Contains(err.Error(), "response exceeds") {
			return fmt.Errorf("read %s response: %w", label, err)
		}
		return &RemoteTransientError{
			Provider: label, Message: cleanReviewText("read response: "+err.Error(), maxReviewMessageChars),
			RateLimits: remoteRateLimitSnapshot(response.Header),
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return remoteStatusError(label, response.StatusCode, responseBody, response.Header)
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return &RemoteTransientError{
			Provider: label, Message: cleanReviewText("decode response: "+err.Error(), maxReviewMessageChars),
			RateLimits: remoteRateLimitSnapshot(response.Header),
		}
	}
	return nil
}

func remoteStatusError(label string, status int, body []byte, headers http.Header) error {
	var payload struct {
		Error struct {
			Message  string              `json:"message"`
			Code     json.RawMessage     `json:"code"`
			Type     string              `json:"type"`
			Status   string              `json:"status"`
			Details  []remoteErrorDetail `json:"details"`
			Metadata struct {
				ErrorType string `json:"error_type"`
			} `json:"metadata"`
		} `json:"error"`
		Message   string          `json:"message"`
		Code      json.RawMessage `json:"code"`
		ErrorType string          `json:"error_type"`
	}
	message := ""
	if json.Unmarshal(body, &payload) == nil {
		message = cleanReviewText(payload.Error.Message, maxReviewMessageChars)
		if message == "" {
			message = cleanReviewText(payload.Message, maxReviewMessageChars)
		}
	}
	code := firstRemoteErrorCode(
		payload.Error.Metadata.ErrorType, remoteErrorCode(payload.Error.Code), payload.Error.Type,
		payload.Error.Status, remoteErrorCode(payload.Code), payload.ErrorType,
	)
	requestTooLarge := remoteRequestTooLarge(status, code, message)
	explicitNoRetry := strings.EqualFold(strings.TrimSpace(headers.Get("x-should-retry")), "false")
	if status == http.StatusTooManyRequests || requestTooLarge {
		limit, requested := remoteTokenLimit(message)
		retryAfter := remoteRetryAfter(headers.Get("Retry-After"), message)
		if retryAfter == 0 {
			retryAfter = remoteRetryInfoDelay(payload.Error.Details)
		}
		return &RemoteRateLimitError{
			Provider: label, StatusCode: status, Message: message, Code: code, LimitTokens: limit, RequestedTokens: requested,
			RetryAfter:      retryAfter,
			RequestTooLarge: requestTooLarge || limit > 0 && requested > limit,
			Permanent:       explicitNoRetry || permanentRemoteQuota(label, code, message, payload.Error.Details, retryAfter),
			RateLimits:      remoteRateLimitSnapshot(headers),
		}
	}
	if remoteTransientStatus(status, headers) || !explicitNoRetry && status == http.StatusConflict && strings.EqualFold(strings.TrimSpace(code), "ABORTED") {
		retryAfter := remoteRetryAfter(headers.Get("Retry-After"), message)
		if retryAfter == 0 {
			retryAfter = remoteRetryInfoDelay(payload.Error.Details)
		}
		return &RemoteTransientError{
			Provider: label, StatusCode: status, Message: message, RetryAfter: retryAfter,
			RateLimits: remoteRateLimitSnapshot(headers),
		}
	}
	if message != "" {
		return fmt.Errorf("%s review failed with HTTP %d: %s", label, status, message)
	}
	return fmt.Errorf("%s review failed with HTTP %d", label, status)
}

func remoteRequestTooLarge(status int, code, message string) bool {
	if status == http.StatusRequestEntityTooLarge {
		return true
	}
	value := strings.ToLower(strings.TrimSpace(code + " " + message))
	for _, marker := range []string{
		"context_length_exceeded", "context window exceeded", "maximum context length",
		"request_too_large", "request too large", "payload too large", "input too long", "prompt is too long",
		"string_too_long", "token_limit_exceeded", "max_tokens_exceeded",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func remoteErrorCode(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return cleanReviewText(value, 100)
	}
	return cleanReviewText(string(raw), 100)
}

func firstRemoteErrorCode(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func permanentRemoteQuota(label, code, message string, details []remoteErrorDetail, retryAfter time.Duration) bool {
	value := strings.ToLower(strings.TrimSpace(code + " " + message))
	if strings.EqualFold(strings.TrimSpace(label), "Gemini") && strings.Contains(value, "quota_exceeded") {
		return true
	}
	for _, detail := range details {
		for _, violation := range detail.Violations {
			quota := strings.ToLower(violation.QuotaMetric + " " + violation.QuotaID)
			if strings.Contains(quota, "perday") || strings.Contains(quota, "per_day") || strings.Contains(quota, "daily") || strings.Contains(quota, "billing") || strings.Contains(quota, "spend") {
				return true
			}
		}
	}
	// Gemini's ordinary RESOURCE_EXHAUSTED response can include generic plan
	// wording even for a rolling limit. RetryInfo is the provider's explicit
	// indication that this particular limit replenishes.
	if strings.EqualFold(strings.TrimSpace(label), "Gemini") && retryAfter > 0 {
		return false
	}
	for _, marker := range []string{
		"insufficient_quota", "billing_hard_limit", "billing_not_active",
		"daily_quota_exceeded", "spend_limit", "credits_exhausted",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	if strings.Contains(value, "check your plan and billing") {
		return true
	}
	return false
}

func remoteTransientStatus(status int, headers http.Header) bool {
	directive := strings.ToLower(strings.TrimSpace(headers.Get("x-should-retry")))
	if directive == "false" {
		return false
	}
	if directive == "true" {
		return true
	}
	switch status {
	case http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		return true
	default:
		return false
	}
}

type remoteErrorDetail struct {
	Type       string `json:"@type"`
	RetryDelay string `json:"retryDelay"`
	Violations []struct {
		QuotaMetric string `json:"quotaMetric"`
		QuotaID     string `json:"quotaId"`
	} `json:"violations"`
}

func remoteRetryInfoDelay(details []remoteErrorDetail) time.Duration {
	for _, detail := range details {
		if !strings.HasSuffix(detail.Type, "/google.rpc.RetryInfo") {
			continue
		}
		if delay, err := time.ParseDuration(strings.TrimSpace(detail.RetryDelay)); err == nil && delay > 0 {
			return delay
		}
	}
	return 0
}

type remoteRateLimitHeaderSet struct {
	limit     string
	remaining string
	reset     string
}

func remoteRateLimitSnapshot(headers http.Header) RateLimitSnapshot {
	requestsKnown, requestLimit, requestsRemaining, requestsReset := remoteRateLimitDimension(headers, []remoteRateLimitHeaderSet{
		{limit: "x-ratelimit-limit-requests", remaining: "x-ratelimit-remaining-requests", reset: "x-ratelimit-reset-requests"},
		{limit: "anthropic-ratelimit-requests-limit", remaining: "anthropic-ratelimit-requests-remaining", reset: "anthropic-ratelimit-requests-reset"},
		// These de-facto generic triples describe request quota units. A lone
		// remaining header is deliberately ignored because its unit is unknown.
		{limit: "ratelimit-limit", remaining: "ratelimit-remaining", reset: "ratelimit-reset"},
		{limit: "x-ratelimit-limit", remaining: "x-ratelimit-remaining", reset: "x-ratelimit-reset"},
		{limit: "x-rate-limit-limit", remaining: "x-rate-limit-remaining", reset: "x-rate-limit-reset"},
	})
	tokensKnown, tokenLimit, tokensRemaining, tokensReset := remoteRateLimitDimension(headers, []remoteRateLimitHeaderSet{
		{limit: "x-ratelimit-limit-tokens", remaining: "x-ratelimit-remaining-tokens", reset: "x-ratelimit-reset-tokens"},
		{limit: "x-ratelimit-limit-project-tokens", remaining: "x-ratelimit-remaining-project-tokens", reset: "x-ratelimit-reset-project-tokens"},
		// Anthropic's input and output token headers describe independent
		// buckets. Collapsing them into one combined estimate can wait forever
		// even when a request fits both dimensions. Use only its unified effective
		// token triple here; separate input/output scheduling can be added later.
		{limit: "anthropic-ratelimit-tokens-limit", remaining: "anthropic-ratelimit-tokens-remaining", reset: "anthropic-ratelimit-tokens-reset"},
	})
	return RateLimitSnapshot{
		RequestsKnown: requestsKnown, LimitRequests: requestLimit,
		RemainingRequests: requestsRemaining, ResetRequests: requestsReset,
		TokensKnown: tokensKnown, LimitTokens: tokenLimit,
		RemainingTokens: tokensRemaining, ResetTokens: tokensReset,
	}
}

func remoteRateLimitDimension(headers http.Header, candidates []remoteRateLimitHeaderSet) (bool, int, int, time.Duration) {
	known, limit, remaining := false, 0, 0
	var reset time.Duration
	for _, candidate := range candidates {
		candidateLimit, limitOK := positiveHeaderIntOK(headers, candidate.limit)
		candidateRemaining, remainingOK := nonNegativeHeaderInt(headers, candidate.remaining)
		if !limitOK || !remainingOK {
			continue
		}
		if !known || candidateLimit < limit {
			limit = candidateLimit
		}
		if !known || candidateRemaining < remaining {
			remaining = candidateRemaining
		}
		if candidateReset := rateLimitResetDuration(headers.Get(candidate.reset)); candidateReset > reset {
			reset = candidateReset
		}
		known = true
	}
	return known, limit, remaining, reset
}

func positiveHeaderIntOK(headers http.Header, name string) (int, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(headers.Get(name)))
	return value, err == nil && value > 0
}

func nonNegativeHeaderInt(headers http.Header, name string) (int, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(headers.Get(name)))
	return value, err == nil && value >= 0
}

func rateLimitResetDuration(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if duration, err := time.ParseDuration(value); err == nil && duration > 0 {
		return duration
	}
	if timestamp, err := time.Parse(time.RFC3339Nano, value); err == nil {
		if duration := time.Until(timestamp); duration > 0 {
			return duration
		}
	}
	if timestamp, err := http.ParseTime(value); err == nil {
		if duration := time.Until(timestamp); duration > 0 {
			return duration
		}
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		// De-facto X-RateLimit-Reset headers use either delay-seconds or a Unix
		// timestamp. A past epoch value means the window has already reset; it
		// must not be misread as a multi-decade delay.
		if seconds >= 1_000_000_000 {
			if duration := time.Until(time.Unix(int64(seconds), 0)); duration > 0 {
				return duration
			}
			return 0
		}
		return time.Duration(seconds * float64(time.Second))
	}
	return 0
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
	response := remoteUsageResponse(inputTokens, outputTokens, 0, duration, RateLimitSnapshot{})
	if strings.TrimSpace(content) == "" {
		return response, &RemoteTransientError{Provider: label, Message: "empty structured response"}
	}
	response.Message.Content = content
	return response, nil
}

func remoteUsageResponse(inputTokens, outputTokens, reasoningTokens int, duration time.Duration, rateLimits RateLimitSnapshot) ollamaChatResponse {
	return ollamaChatResponse{
		Done: true, PromptEvalCount: inputTokens, EvalCount: outputTokens, ReasoningCount: reasoningTokens,
		TotalDuration: duration.Nanoseconds(), RateLimits: rateLimits,
	}
}
