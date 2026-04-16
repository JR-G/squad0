package memory_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCosineSimilarity_IdenticalVectors_ReturnsOne(t *testing.T) {
	t.Parallel()

	vec := []float32{1.0, 2.0, 3.0}

	result := memory.CosineSimilarity(vec, vec)

	assert.InDelta(t, 1.0, float64(result), 0.0001)
}

func TestCosineSimilarity_OrthogonalVectors_ReturnsZero(t *testing.T) {
	t.Parallel()

	vecA := []float32{1.0, 0.0}
	vecB := []float32{0.0, 1.0}

	result := memory.CosineSimilarity(vecA, vecB)

	assert.InDelta(t, 0.0, float64(result), 0.0001)
}

func TestCosineSimilarity_OppositeVectors_ReturnsNegativeOne(t *testing.T) {
	t.Parallel()

	vecA := []float32{1.0, 0.0}
	vecB := []float32{-1.0, 0.0}

	result := memory.CosineSimilarity(vecA, vecB)

	assert.InDelta(t, -1.0, float64(result), 0.0001)
}

func TestCosineSimilarity_ZeroVector_ReturnsZero(t *testing.T) {
	t.Parallel()

	vecA := []float32{0.0, 0.0}
	vecB := []float32{1.0, 2.0}

	result := memory.CosineSimilarity(vecA, vecB)

	assert.Equal(t, float32(0), result)
}

func TestCosineSimilarity_DifferentLengths_ReturnsZero(t *testing.T) {
	t.Parallel()

	result := memory.CosineSimilarity([]float32{1.0}, []float32{1.0, 2.0})

	assert.Equal(t, float32(0), result)
}

func TestCosineSimilarity_EmptyVectors_ReturnsZero(t *testing.T) {
	t.Parallel()

	result := memory.CosineSimilarity([]float32{}, []float32{})

	assert.Equal(t, float32(0), result)
}

func TestSerialiseVector_RoundTrip(t *testing.T) {
	t.Parallel()

	original := []float32{1.5, -2.3, 0.0, 3.14159, math.MaxFloat32}

	serialised := memory.SerialiseVector(original)
	deserialised, err := memory.DeserialiseVector(serialised)

	require.NoError(t, err)
	require.Len(t, deserialised, len(original))
	for i := range original {
		assert.InDelta(t, float64(original[i]), float64(deserialised[i]), 0.0001)
	}
}

func TestSerialiseVector_Empty_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	serialised := memory.SerialiseVector([]float32{})

	assert.Empty(t, serialised)
}

func TestDeserialiseVector_InvalidLength_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := memory.DeserialiseVector([]byte{1, 2, 3})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a multiple of 4")
}

func TestDeserialiseVector_Nil_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	result, err := memory.DeserialiseVector(nil)

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestEmbedder_Embed_ReturnsVector(t *testing.T) {
	t.Parallel()

	expectedVec := []float32{0.1, 0.2, 0.3}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "/api/embeddings", req.URL.Path)
		assert.Equal(t, "POST", req.Method)

		resp := map[string][]float32{"embedding": expectedVec}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	defer server.Close()

	embedder := memory.NewEmbedder(server.URL, "test-model")
	result, err := embedder.Embed(context.Background(), "test text")

	require.NoError(t, err)
	assert.Equal(t, expectedVec, result)
}

func TestEmbedder_Embed_ServerError_ReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	embedder := memory.NewEmbedder(server.URL, "test-model")
	_, err := embedder.Embed(context.Background(), "test text")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestEmbedder_Embed_EmptyEmbedding_ReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		resp := map[string][]float32{"embedding": {}}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	defer server.Close()

	embedder := memory.NewEmbedder(server.URL, "test-model")
	_, err := embedder.Embed(context.Background(), "test text")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty embedding")
}

func TestEmbedder_Embed_Unreachable_ReturnsError(t *testing.T) {
	t.Parallel()

	embedder := memory.NewEmbedder("http://localhost:1", "test-model")
	_, err := embedder.Embed(context.Background(), "test text")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Ollama")
}
