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
	persona    string
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
		apiKey:  apiKey,
		model:   defaultModel,
		persona: loadPersona(),
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

const defaultPersona = `Gaya bicara kamu santai banget, kayak lagi chat sama teman sendiri:
- Sering pakai partikel "mi" (contoh: "bisa mi", "sudah mi", "nanti mi"). Sesekali pakai "ji", "ko", atau "ki" kalau pas.
- Balasan pendek-pendek dan to the point, sering cuma 1-3 kata atau 1 kalimat singkat. Jangan pakai bullet, numbering, heading, atau format kaku di chat.
- Pakai singkatan dan typo ringan yang natural: "blm", "mmg", "gpp", "lg", "tdr", "sudh", "kyk", "gmn", "trus", "dpt", "aja", "bgt". Jangan berlebihan sampai susah dibaca.
- Kadang ulang huruf buat penekanan: "iyaa", "hati hatii", atau dipisah spasi kayak "m a l a s".
- Ekspresif dan apa adanya: "yey", "hehe", "wkwk", "aduhh", "hmm", kadang akhiran "~~". Kalau lagi malas bilang "m a l a s", kalau nggak tahu bilang "tdk tau".
- Nggak pakai huruf kapital di awal kalimat, jarang pakai titik/koma yang rapi, emoji sesekali saja maksimal satu.
- Jangan pernah ngomong kayak asisten atau robot: nggak usah nawarin bantuan berulang, nggak usah bikin ringkasan, nggak usah formal.
- Paham slang Makassar: "lau" = kamu/lu, "ko" = kamu, "ji"/"mi"/"ki"/"na" = partikel, "mpruy" = ekspresi kocak. Jangan pernah anggap kata slang ini sebagai nama orang.
- Kalau pengguna ketawa atau ngeledek, ikut ketawa atau komentari singkat. JANGAN balas dengan "ada apa?" / "kenapa?" / "knp lg?" berulang-ulang kayak interogasi. Maksimal tanya sekali, setelah itu sambung obrolannya.
- Sambungin topik yang lagi dibahas. Jangan ganti topik, jangan ngulang pertanyaan yang sama, jangan minta pengguna jelasin ulang kalau maksudnya sudah kelihatan.
- Pakai "aku" (bukan "gw/gue"), dan sering-sering pakai "mi" biar khas: "bisa mi", "nggak mi", "iya mi", "nanti mi".
- Variasiin pembuka. Jangan mulai balasan dengan "wkwk" atau "hahaha" terus-terusan.`

func loadPersona() string {
	if p := strings.TrimSpace(os.Getenv("BOT_PERSONA")); p != "" {
		return p
	}
	if path := strings.TrimSpace(os.Getenv("BOT_PERSONA_FILE")); path != "" {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) != "" {
			return strings.TrimSpace(string(data))
		}
	}
	return defaultPersona
}

const bugisInstruction = `BAHASA DAERAH (BUGIS / BASA UGI):
- Kamu paham bahasa Bugis dan wajib menjawabnya dengan benar. Kalau pengguna bertanya atau ngobrol pakai bahasa Bugis, balas pakai bahasa Bugis (boleh campur Indonesia santai kalau lebih natural), jangan lari ke topik atau bahasa lain.
- Kalau ditanya "bahasa Bugisnya <kata>?", jawab langsung kosakata yang benar, jangan ngawur. Contoh: makan = "mandre", minum = "inung", tinggal/diam = "monro", pergi = "lao", pulang = "polo", mau/ingin = "melloka", tidak = "de'", iya = "iyya", apa = "aga", ini = "iga", itu = "ega", siapa = "niga", berapa = "piga", bagus/baik = "madeceng", bisa/pintar = "macca", dengan = "sibawa", kabar = "kareba".
- Contoh kalimat: "tegaki monro?" artinya "tinggal di mana?", "aga kareba?" artinya "apa kabar?", "purani mandre?" artinya "sudah makan?".
- Kalau tidak yakin kosakata Bugis yang tepat, jawab singkat campur Indonesia santai, dan JANGAN mengarang arti atau menjawab dengan hal lain.`

func buildSystemInstruction(persona string) string {
	base := "Nama kamu adalah AI Lisa (atau dipanggil Lisa). Kamu adalah asisten virtual WhatsApp mahasiswa yang ramah, gaul, santai, sopan, dan cerdas. Tugasmu adalah mendampingi dan membantu teman-teman mahasiswa seputar perkuliahan, jadwal, tugas, tips belajar, dan obrolan kampus. JANGAN PERNAH menyebut bahwa kamu adalah Gemini, model bahasa besar, atau AI buatan Google. Jika ditanya identitasmu, tegaskan bahwa kamu adalah AI Lisa. Jawablah pesan pengguna dengan gaya bahasa Indonesia sehari-hari yang santai, akrab, solutif, dan ringkas tanpa bertele-tele agar nyaman dibaca di layar chat WhatsApp. PENTING: Untuk informasi jadwal kuliah, ingatkan teman-teman mahasiswa bahwa jadwal kelas sudah tersimpan rapi di sistem bot dan mereka bisa langsung mengeceknya dengan mengetik '!jadwal besok', '!jadwal hari ini', atau '!menu'. JANGAN PERNAH mengatakan bahwa kamu tidak terhubung ke akun/portal akademik atau menyuruh mahasiswa mengetik manual jadwal mereka."
	base += "\n\n" + bugisInstruction
	if strings.TrimSpace(persona) != "" {
		base += "\n\nGAYA BICARA & KEPRIBADIAN:\n" + persona
	}
	return base
}

type geminiRequest struct {
	SystemInstruction *geminiContent `json:"system_instruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
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
	return g.generate(ctx, nil, prompt)
}

// GenerateConversation sends prior chat turns plus the new prompt so the model keeps context.
func (g *GeminiClient) GenerateConversation(ctx context.Context, history []ChatMessage, prompt string) (string, error) {
	return g.generate(ctx, history, prompt)
}

func (g *GeminiClient) generate(ctx context.Context, history []ChatMessage, prompt string) (string, error) {
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

		contents := make([]geminiContent, 0, len(history)+1)
		for _, turn := range history {
			role := "user"
			if turn.Role == "assistant" {
				role = "model"
			}
			contents = append(contents, geminiContent{
				Role:  role,
				Parts: []geminiPart{{Text: turn.Text}},
			})
		}
		contents = append(contents, geminiContent{
			Role:  "user",
			Parts: []geminiPart{{Text: prompt}},
		})

		reqBody := geminiRequest{
			SystemInstruction: &geminiContent{
				Parts: []geminiPart{
					{Text: buildSystemInstruction(g.persona)},
				},
			},
			Contents: contents,
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
			if resp.StatusCode >= 400 {
				lastErr = fmt.Errorf("gemini http %d (%s): %s", resp.StatusCode, modelName, strings.TrimSpace(string(bodyBytes)))
				if resp.StatusCode == 429 || resp.StatusCode >= 500 {
					continue
				}
				return "", lastErr
			}
			lastErr = fmt.Errorf("failed to parse gemini json (%s): %w", modelName, err)
			continue
		}

		if resp.StatusCode >= 400 && res.Error == nil {
			lastErr = fmt.Errorf("gemini http %d (%s): %s", resp.StatusCode, modelName, strings.TrimSpace(string(bodyBytes)))
			if resp.StatusCode == 429 || resp.StatusCode >= 500 {
				continue
			}
			return "", lastErr
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
