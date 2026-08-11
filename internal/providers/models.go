package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const maxRemoteModels = 2000

type RemoteModel struct {
	ID          string
	DisplayName string
}

type ModelListOptions struct {
	Provider   Kind
	Label      string
	APIKey     string
	BaseURL    string
	Timeout    time.Duration
	HTTPClient *http.Client
}

func ListModels(ctx context.Context, options ModelListOptions) ([]RemoteModel, error) {
	if strings.TrimSpace(options.APIKey) == "" {
		return nil, errors.New("list remote models: API key is not available")
	}
	endpoint, keyHeader, keyPrefix, headers, err := modelListRequest(options)
	if err != nil {
		return nil, err
	}
	client := options.HTTPClient
	if client == nil {
		timeout := options.Timeout
		if timeout <= 0 || timeout > 30*time.Second {
			timeout = 15 * time.Second
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		client = &http.Client{
			Transport: transport, Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create %s model catalogue request: %w", modelListLabel(options), err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "ComplyScan")
	request.Header.Set(keyHeader, keyPrefix+options.APIKey)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request %s model catalogue: %w", modelListLabel(options), err)
	}
	defer response.Body.Close()
	body, err := readLimited(response.Body, maxRemoteResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("read %s model catalogue: %w", modelListLabel(options), err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s model catalogue failed with HTTP %d", modelListLabel(options), response.StatusCode)
	}
	models, err := decodeRemoteModels(options.Provider, body)
	if err != nil {
		return nil, fmt.Errorf("decode %s model catalogue: %w", modelListLabel(options), err)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("%s model catalogue returned no text-generation models", modelListLabel(options))
	}
	return models, nil
}

func modelListRequest(options ModelListOptions) (string, string, string, map[string]string, error) {
	switch options.Provider {
	case OpenAI:
		return "https://api.openai.com/v1/models", "Authorization", "Bearer ", nil, nil
	case Anthropic:
		return "https://api.anthropic.com/v1/models?limit=1000", "x-api-key", "", map[string]string{"anthropic-version": "2023-06-01"}, nil
	case Gemini:
		return "https://generativelanguage.googleapis.com/v1beta/models?pageSize=1000", "x-goog-api-key", "", nil, nil
	case XAI, Groq, Mistral, OpenRouter, Compatible:
		baseURL, err := validateCompatibleBaseURL(options.BaseURL)
		if err != nil {
			return "", "", "", nil, fmt.Errorf("list %s models: %w", modelListLabel(options), err)
		}
		return baseURL + "/models", "Authorization", "Bearer ", nil, nil
	default:
		return "", "", "", nil, fmt.Errorf("model catalogue is unavailable for provider %q", options.Provider)
	}
}

func modelListLabel(options ModelListOptions) string {
	if strings.TrimSpace(options.Label) != "" {
		return strings.TrimSpace(options.Label)
	}
	return string(options.Provider)
}

func decodeRemoteModels(provider Kind, body []byte) ([]RemoteModel, error) {
	if provider == Gemini {
		var payload struct {
			Models []struct {
				Name                       string   `json:"name"`
				BaseModelID                string   `json:"baseModelId"`
				DisplayName                string   `json:"displayName"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		models := make([]RemoteModel, 0, len(payload.Models))
		for _, model := range payload.Models {
			if !containsString(model.SupportedGenerationMethods, "generateContent") {
				continue
			}
			id := strings.TrimSpace(model.BaseModelID)
			if id == "" {
				id = strings.TrimPrefix(strings.TrimSpace(model.Name), "models/")
			}
			models = appendRemoteModel(models, RemoteModel{ID: id, DisplayName: model.DisplayName})
		}
		return models, nil
	}
	type modelRecord struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		DisplayName  string `json:"display_name"`
		Capabilities struct {
			CompletionChat *bool `json:"completion_chat"`
		} `json:"capabilities"`
		Architecture struct {
			OutputModalities []string `json:"output_modalities"`
		} `json:"architecture"`
	}
	var records []modelRecord
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &records); err != nil {
			return nil, err
		}
	} else {
		var payload struct {
			Data []modelRecord `json:"data"`
		}
		if err := json.Unmarshal(trimmed, &payload); err != nil {
			return nil, err
		}
		records = payload.Data
	}
	models := make([]RemoteModel, 0, len(records))
	for _, record := range records {
		if !supportsTextReview(provider, record.ID, record.Capabilities.CompletionChat, record.Architecture.OutputModalities) {
			continue
		}
		display := strings.TrimSpace(record.DisplayName)
		if display == "" {
			display = strings.TrimSpace(record.Name)
		}
		models = appendRemoteModel(models, RemoteModel{ID: record.ID, DisplayName: display})
	}
	return models, nil
}

func supportsTextReview(provider Kind, id string, completionChat *bool, outputModalities []string) bool {
	if completionChat != nil && !*completionChat {
		return false
	}
	if len(outputModalities) > 0 && !containsString(outputModalities, "text") {
		return false
	}
	if provider != OpenAI && provider != XAI {
		return true
	}
	lower := strings.ToLower(id)
	for _, marker := range []string{"embedding", "moderation", "transcrib", "whisper", "tts", "image", "realtime", "audio", "sora"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func appendRemoteModel(models []RemoteModel, model RemoteModel) []RemoteModel {
	model.ID = strings.TrimSpace(model.ID)
	model.DisplayName = strings.TrimSpace(model.DisplayName)
	if model.ID == "" || strings.ContainsAny(model.ID, "\r\n\x00") || len(models) >= maxRemoteModels {
		return models
	}
	for _, existing := range models {
		if strings.EqualFold(existing.ID, model.ID) {
			return models
		}
	}
	return append(models, model)
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
