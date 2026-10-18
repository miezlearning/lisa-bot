package nlu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// GeminiClient implements LLMFallback using Google Gemini (Free Tier via Google AI Studio).
type GeminiClient struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewGeminiClient creates a new Gemini AI client.
func NewGeminiClient(apiKey string) *GeminiClient {
	if apiKey == "" {
		return nil
	}
	defaultModel := os.Getenv("GEMINI_MODEL")
	if defaultModel == "" {
		defaultModel = "gemini-3.5-flash-lite"
	}
	return &GeminiClient{
		apiKey: apiKey,
		model:  defaultModel,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

type geminiRequest struct {
	SystemInstruction *geminiContent `json:"system_instruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

// GenerateResponse queries Gemini API for natural conversation and typo-resilient fallback answers.
func (g *GeminiClient) GenerateResponse(ctx context.Context, prompt string) (string, error) {
	if g == nil || g.apiKey == "" {
		return "", fmt.Errorf("gemini api key not configured")
	}

	candidateModels := []string{g.model}
	fallbacks := []string{"gemini-3.5-flash-lite", "gemini-3.8-flash", "gemini-flash-latest"}
	for _, fb := range fallbacks {
		if fb != g.model {
			candidateModels = append(candidateModels, fb)
		}
	}

	var lastErr error
	for _, modelName := range candidateModels {
		url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", modelName, g.apiKey)

		reqBody := geminiRequest{
			SystemInstruction: &geminiContent{
				Parts: []geminiPart{
					{Text: "Kamu adalah Lisa, asisten perkuliahan WhatsApp mahasiswa yang ramah, sopan, dan cerdas. Jawablah pesan pengguna dengan bahasa Indonesia yang santai dan jelas. Pahami maksud pengguna meskipun ada salah ketik (typo) atau bahasa gaul. Jawablah dengan ringkas dan to the point agar nyaman dibaca di layar chat WhatsApp."},
				},
			},
			Contents: []geminiContent{
				{
					Parts: []geminiPart{
						{Text: prompt},
					},
				},
			},
		}

		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return "", fmt.Errorf("failed to marshal gemini request: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return "", fmt.Errorf("failed to create http request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := g.httpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("gemini http request failed (%s): %w", modelName, err)
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read gemini response body (%s): %w", modelName, err)
			continue
		}

		var res geminiResponse
		if err := json.Unmarshal(bodyBytes, &res); err != nil {
			lastErr = fmt.Errorf("failed to parse gemini json (%s): %w", modelName, err)
			continue
		}

		if res.Error != nil {
			lastErr = fmt.Errorf("gemini api error (%s): %s", modelName, res.Error.Message)
			// If error is 404 model not found or 503 high demand, try next model
			if res.Error.Code == 404 || res.Error.Code == 503 {
				continue
			}
			return "", lastErr
		}

		if len(res.Candidates) > 0 && len(res.Candidates[0].Content.Parts) > 0 {
			reply := strings.TrimSpace(res.Candidates[0].Content.Parts[0].Text)
			if reply != "" {
				return reply, nil
			}
		}

		lastErr = fmt.Errorf("empty response received from gemini model %s", modelName)
	}

	return "", lastErr
}
