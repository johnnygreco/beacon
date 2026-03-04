package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// EmbeddingProvider generates vector embeddings from text.
type EmbeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	ModelName() string
}

// OllamaProvider uses a local Ollama instance for embeddings.
type OllamaProvider struct {
	url   string
	model string
}

// NewOllamaProvider creates a new Ollama embedding provider.
func NewOllamaProvider(url, model string) *OllamaProvider {
	return &OllamaProvider{url: url, model: model}
}

func (o *OllamaProvider) ModelName() string { return o.model }

func (o *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody, _ := json.Marshal(map[string]string{
		"model":  o.model,
		"prompt": text,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", o.url+"/api/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

// OpenAIProvider uses OpenAI's API for embeddings.
type OpenAIProvider struct {
	apiKey     string
	model      string
	dimensions int
}

// NewOpenAIProvider creates a new OpenAI embedding provider.
func NewOpenAIProvider(apiKey, model string, dimensions int) *OpenAIProvider {
	return &OpenAIProvider{apiKey: apiKey, model: model, dimensions: dimensions}
}

func (o *OpenAIProvider) ModelName() string { return o.model }

func (o *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	body := map[string]any{
		"model": o.model,
		"input": text,
	}
	if o.dimensions > 0 {
		body["dimensions"] = o.dimensions
	}

	reqBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai returned %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return result.Data[0].Embedding, nil
}
