package knowledge

import (
	"fmt"
	"strings"

	"whatsapp-bot/internal/database"
	"whatsapp-bot/internal/nlu"
	"whatsapp-bot/internal/router"
)

// Handler answers general inquiries, lab/rules/prohibitions, and manages knowledge base additions.
type Handler struct {
	db  *database.DB
	sc  *nlu.SpellChecker
	llm nlu.LLMFallback
}

// NewHandler creates a new knowledge base module handler with optional LLM fallback.
func NewHandler(db *database.DB, sc *nlu.SpellChecker, llm ...nlu.LLMFallback) *Handler {
	var fallback nlu.LLMFallback
	if len(llm) > 0 {
		fallback = llm[0]
	}
	return &Handler{
		db:  db,
		sc:  sc,
		llm: fallback,
	}
}

func (h *Handler) Name() string {
	return "knowledge"
}

func (h *Handler) CanHandle(result *nlu.NLUResult) bool {
	if result == nil {
		return false
	}
	return result.Intent == nlu.IntentKnowledgeQuery ||
		result.Intent == nlu.IntentKnowledgeAdd ||
		result.Intent == nlu.IntentBotHelp
}

func (h *Handler) Execute(ctx *router.MessageContext) (string, error) {
	result := ctx.NLUResult

	switch result.Intent {
	case nlu.IntentBotHelp:
		return h.handleHelp(ctx)
	case nlu.IntentKnowledgeAdd:
		return h.handleAddInfo(ctx)
	case nlu.IntentKnowledgeQuery:
		return h.handleQuery(ctx)
	default:
		return "Perintah informasi tidak dikenali.", nil
	}
}

func (h *Handler) handleHelp(ctx *router.MessageContext) (string, error) {
	items, err := h.db.FindKnowledge(ctx.Context, "info bot")
	if err == nil && len(items) > 0 {
		return items[0].Content, nil
	}

	return "🤖 *Bantuan Bot Lisa*\n\n" +
		"Perintah yang dapat digunakan:\n" +
		"• Cek Jadwal: *jadwal senin*, *jadwal besok*, *hari ini kuliah apa*\n" +
		"• Status Saat Ini: *sekarang kelas apa?*\n" +
		"• Tanya Info/Aturan: *larangan praktikum apa aja?*, *aturan kuliah*\n" +
		"• Update Info Harian: `@Bot info hari ini <matkul> <catatan>`\n" +
		"• Tambah Pengetahuan: `@Bot tambah info <topik> | <isi info>`\n" +
		"• Simpan Jadwal: `@Bot simpan jadwal` diikuti teks jadwal.", nil
}

func (h *Handler) handleAddInfo(ctx *router.MessageContext) (string, error) {
	payload := ctx.NLUResult.Entities.RawPayload
	if strings.TrimSpace(payload) == "" {
		return "⚠️ Format tidak lengkap. Gunakan contoh:\n`@Bot tambah info beasiswa | Pendaftaran dibuka sampai akhir bulan.`", nil
	}

	parts := strings.SplitN(payload, "|", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "⚠️ Format tidak valid. Pastikan menggunakan pemisah pipa (`|`).\nContoh: `@Bot tambah info beasiswa | Pendaftaran dibuka sampai akhir bulan.`", nil
	}

	topic := strings.TrimSpace(parts[0])
	content := strings.TrimSpace(parts[1])
	keywords := strings.ToLower(topic)

	if err := h.db.UpsertKnowledge(ctx.Context, topic, keywords, content); err != nil {
		return "", fmt.Errorf("failed to save knowledge: %w", err)
	}

	// Re-harvest vocabulary so new words are known to spellchecker
	_ = database.HarvestVocabulary(ctx.Context, h.db, h.sc)

	return fmt.Sprintf("✅ *Pengetahuan Baru Berhasil Disimpan!*\n\n📌 Topik: *%s*\n📄 Isi: %s", topic, content), nil
}

func (h *Handler) handleQuery(ctx *router.MessageContext) (string, error) {
	topic := ctx.NLUResult.Entities.KnowledgeTopic
	if topic == "" {
		topic = ctx.NLUResult.Normalized
	}
	topicClean := strings.TrimSpace(strings.ToLower(topic))

	// 1. Direct search on the full topic phrase
	items, err := h.db.FindKnowledge(ctx.Context, topicClean)
	if err != nil {
		return "", err
	}

	if len(items) > 0 {
		return items[0].Content, nil
	}

	// 2. Secondary search on significant content words (excluding generic query stopwords)
	stopwords := map[string]bool{
		"info": true, "apa": true, "itu": true, "ini": true, "kaya": true, "kayak": true,
		"tentang": true, "tanya": true, "dong": true, "mau": true, "coba": true, "cek": true,
		"tahu": true, "soal": true, "mengenai": true, "ada": true, "gimana": true, "bagaimana": true,
		"tolong": true, "bisa": true, "sebutin": true, "disebutin": true, "aja": true, "saja": true,
	}

	words := strings.Fields(topicClean)
	for _, w := range words {
		w = strings.Trim(w, "?!.,")
		if len(w) >= 3 && !stopwords[w] {
			items, err := h.db.FindKnowledge(ctx.Context, w)
			if err == nil && len(items) > 0 {
				return items[0].Content, nil
			}
		}
	}

	// 3. Fallback ke LLM Eksternal (Gemini Free) jika diaktifkan
	if h.llm != nil {
		fmt.Printf("🤖 [Gemini AI] Memproses pertanyaan bebas: %q...\n", ctx.RawMessage)
		llmPrompt := fmt.Sprintf("Pengguna bertanya: %q (teks normalisasi: %q). Jawablah pertanyaan ini sebagai asisten perkuliahan.", ctx.RawMessage, topicClean)
		llmResp, err := h.llm.GenerateResponse(ctx.Context, llmPrompt)
		if err == nil && strings.TrimSpace(llmResp) != "" {
			fmt.Println("✨ [Gemini AI] Berhasil menghasilkan respons cerdas.")
			return llmResp, nil
		}
		fmt.Printf("⚠️ [Gemini AI Error]: %v\n", err)
	}

	return fmt.Sprintf("Maaf, informasi mengenai *%s* belum ada di database. Kamu bisa menambahkannya dengan perintah:\n`@Bot tambah info %s | <isi informasi>`", topic, topic), nil
}
