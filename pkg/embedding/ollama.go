package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaProvider calls the local Ollama HTTP API to generate embeddings.
// Default model: nomic-embed-text (768 dimensions).
type OllamaProvider struct {
	baseURL    string
	model      string
	httpClient *http.Client
	dims       int
}

// NewOllama creates an Ollama-backed embedding provider.
func NewOllama(cfg Config) *OllamaProvider {
	url := cfg.OllamaURL
	if url == "" {
		url = "http://localhost:11434"
	}
	model := cfg.OllamaModel
	if model == "" {
		model = "nomic-embed-text"
	}
	return &OllamaProvider{
		baseURL: url,
		model:   model,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		dims: 768, // nomic-embed-text default; updated on first call
	}
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

func (o *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(ollamaEmbedRequest{Model: o.model, Prompt: text})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		// Retry once.
		resp, err = o.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("ollama embed: %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed: status %d: %s", resp.StatusCode, string(data))
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w", err)
	}
	if len(result.Embedding) > 0 {
		o.dims = len(result.Embedding)
	}
	return result.Embedding, nil
}

func (o *OllamaProvider) Dimensions() int { return o.dims }
func (o *OllamaProvider) Name() string    { return "ollama:" + o.model }
