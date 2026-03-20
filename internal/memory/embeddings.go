package memory

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"
)

// Embedder generates text embeddings via a local Ollama instance.
type Embedder struct {
	httpClient *http.Client
	baseURL    string
	model      string
}

// NewEmbedder creates an Embedder configured to call the Ollama API at the
// given base URL using the specified model.
func NewEmbedder(baseURL, model string) *Embedder {
	return &Embedder{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    baseURL,
		model:      model,
	}
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaResponse struct {
	Embedding []float32 `json:"embedding"`
}

// Embed generates an embedding vector for the given text.
func (emb *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(ollamaRequest{Model: emb.model, Prompt: text})
	if err != nil {
		return nil, fmt.Errorf("marshalling embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, emb.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := emb.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Ollama embeddings API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama embeddings API returned status %d", resp.StatusCode)
	}

	var result ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding embed response: %w", err)
	}

	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("Ollama returned empty embedding")
	}

	return result.Embedding, nil
}

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns a value between -1 and 1. Panics if the vectors have different
// lengths.
func CosineSimilarity(vecA, vecB []float32) float32 {
	if len(vecA) != len(vecB) {
		panic(fmt.Sprintf("vector length mismatch: %d vs %d", len(vecA), len(vecB)))
	}

	var dot, magA, magB float32
	for i := range vecA {
		dot += vecA[i] * vecB[i]
		magA += vecA[i] * vecA[i]
		magB += vecB[i] * vecB[i]
	}

	magA = float32(math.Sqrt(float64(magA)))
	magB = float32(math.Sqrt(float64(magB)))

	if magA == 0 || magB == 0 {
		return 0
	}

	return dot / (magA * magB)
}

// SerialiseVector converts a float32 slice to a byte slice for storage as
// a SQLite BLOB. Uses little-endian encoding.
func SerialiseVector(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, val := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(val))
	}
	return buf
}

// DeserialiseVector converts a byte slice from a SQLite BLOB back to a
// float32 slice.
func DeserialiseVector(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("vector data length %d is not a multiple of 4", len(data))
	}

	vec := make([]float32, len(data)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return vec, nil
}
