package knowledge

import (
	"fmt"
	"strings"
	"time"

	"whatsapp-bot/internal/database"
	"whatsapp-bot/internal/nlu"
	"whatsapp-bot/internal/router"
)

// Handler answers general inquiries, lab/rules/prohibitions, and manages knowledge base additions.
type Handler struct {
	db        *database.DB
	sc        *nlu.SpellChecker
	llm       nlu.LLMFallback
	startTime time.Time
}

// NewHandler creates a new knowledge base module handler with optional LLM fallback.
func NewHandler(db *database.DB, sc *nlu.SpellChecker, llm ...nlu.LLMFallback) *Handler {
	var fallback nlu.LLMFallback
	if len(llm) > 0 {
		fallback = llm[0]
	}
	return &Handler{
		db:        db,
		sc:        sc,
		llm:       fallback,
		startTime: time.Now(),
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
		result.Intent == nlu.IntentBotHelp ||
		result.Intent == nlu.IntentConversational
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
	case nlu.IntentConversational:
		return h.handleConversation(ctx)
	default:
		return "Perintah informasi tidak dikenali.", nil
	}
}

func (h *Handler) handleConversation(ctx *router.MessageContext) (string, error) {
	// 1. Jika Otak AI aktif, jawab secara cerdas & natural dengan konteks percakapan
	if h.llm != nil {
		fmt.Printf("dY- [AI Lisa Brain] Menjawab percakapan: %q...\n", ctx.RawMessage)
		var resp string
		var err error
		if conv, ok := h.llm.(nlu.ConversationalLLM); ok && len(ctx.History) > 0 {
			resp, err = conv.GenerateConversation(ctx.Context, ctx.History, ctx.CleanMessage)
		} else {
			resp, err = h.llm.GenerateResponse(ctx.Context, ctx.CleanMessage)
		}
		if err == nil && strings.TrimSpace(resp) != "" {
			fmt.Println("[AI Lisa Brain] Berhasil membalas percakapan.")
			return resp, nil
		}
		fmt.Printf("[AI Lisa Brain Error]: %v\n", err)
	}

	// 2. Fallback jika tanpa Gemini AI (persona Lisa yang ramah)
	lower := strings.ToLower(ctx.CleanMessage)
	if strings.Contains(lower, "siapa") || strings.Contains(lower, "nama") || strings.Contains(lower, "tugas") {
		return "Halo! Aku *Lisa*, asisten perkuliahan WhatsApp kamu. 🎓\n\n" +
			"Aku bisa bantu mengecek jadwal kuliah harian, aturan praktikum/lab, dan informasi seputar perkuliahan.\n" +
			"Ketik *menu* untuk melihat panduan lengkap apa saja yang bisa aku bantu ya!", nil
	}

	if strings.Contains(lower, "halo") || strings.Contains(lower, "hai") || strings.Contains(lower, "pagi") || strings.Contains(lower, "siang") || strings.Contains(lower, "malam") {
		return "Halo! Ada yang bisa Lisa bantu seputar jadwal atau info kuliah hari ini? 😊\nKetik *jadwal hari ini* atau *menu* ya!", nil
	}

	return "Halo! Lisa siap membantu seputar jadwal kuliah dan informasi perkuliahan. Coba tanyakan *jadwal besok* atau ketik *menu* untuk panduan lengkapnya ya!", nil
}

func (h *Handler) handleHelp(ctx *router.MessageContext) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(ctx.CleanMessage))
	if lower == "ping" || lower == "status" {
		uptime := formatDuration(time.Since(h.startTime))
		return fmt.Sprintf("🏓 *PONG!*\n\n"+
			"╭─「 ⚡ *SYSTEM STATUS* 」\n"+
			"│ 🤖 *Bot*      : AI Lisa\n"+
			"│ 🟢 *Status*   : Online & Normal\n"+
			"│ ⏱️ *Uptime*   : %s\n"+
			"│ 🧠 *Engine*   : Pure Go NLU + Auto-Corrector\n"+
			"│ 📦 *Storage*  : SQLite3 Auto-Sync\n"+
			"│ 🕒 *Server*   : %s\n"+
			"╰────────────────────────┈", uptime, time.Now().Format("15:04:05 MST")), nil
	}

	// Dynamic greeting based on hour of day
	hour := time.Now().Hour()
	var greeting string
	switch {
	case hour >= 4 && hour < 11:
		greeting = "Selamat Pagi 🌅"
	case hour >= 11 && hour < 15:
		greeting = "Selamat Siang ☀️"
	case hour >= 15 && hour < 18:
		greeting = "Selamat Sore 🌇"
	default:
		greeting = "Selamat Malam 🌙"
	}

	userName := ctx.SenderName
	if userName == "" {
		userName = "Teman Mahasiswa"
	}

	phoneOrID := ctx.SenderPhone
	if phoneOrID == "" {
		phoneOrID = ctx.SenderJID.User
	}

	userRole := "🎓 Mahasiswa"
	isAdmin := h.db.IsAdmin(ctx.Context, phoneOrID) || h.db.IsAdmin(ctx.Context, ctx.SenderJID.User)
	if isAdmin {
		userRole = "👑 Admin Bot"
	}

	uptime := formatDuration(time.Since(h.startTime))

	adminSection := ""
	if isAdmin {
		adminSection = "╭─「 🛠️ *PENGATURAN ADMIN* 」\n" +
			"│ ⭔ `!izinkan grup` _(Whitelist grup ini)_\n" +
			"│ ⭔ `!cabut grup` _(Nonaktifkan di grup)_\n" +
			"│ ⭔ `!daftar grup` _(Lihat grup aktif)_\n" +
			"│ ⭔ `!tambah admin <nomor>`\n" +
			"│ ⭔ `!daftar admin` _(Lihat daftar admin)_\n" +
			"╰────────────────────────┈\n\n"
	}

	menu := fmt.Sprintf("Halo, *%s*! %s 👋\n\n"+
		"╭─「 🤖 *AI LISA DASHBOARD* 」\n"+
		"│ 👤 *User*    : %s\n"+
		"│ 🏷️ *Role*    : %s\n"+
		"│ ⏱️ *Uptime*  : %s\n"+
		"│ ⚡ *Prefix*  : [ `!` `.` `/` `#` ]\n"+
		"│ 💡 *Mode*    : Bisa pakai prefix / tanpa prefix\n"+
		"╰────────────────────────┈\n\n"+
		"╭─「 📅 *JADWAL KULIAH* 」\n"+
		"│ ⭔ `!jadwal senin` _(Cek hari tertentu)_\n"+
		"│ ⭔ `!jadwal besok` _(Cek jadwal besok)_\n"+
		"│ ⭔ `!sekarang kelas apa` _(Cek kelas saat ini)_\n"+
		"│ ⭔ `!kapan kelas pih` _(Cari matkul tertentu)_\n"+
		"│ ⭔ `!info hari ini <matkul> <catatan>`\n"+
		"╰────────────────────────┈\n\n"+
		"╭─「 📌 *ATURAN & TATA TERTIB* 」\n"+
		"│ ⭔ `!larangan praktikum` _(Tata tertib lab)_\n"+
		"│ ⭔ `!aturan lab` _(Ketentuan kuliah)_\n"+
		"│ ⭔ `!tambah info <topik> | <isi>`\n"+
		"╰────────────────────────┈\n\n"+
		"%s"+
		"╭─「 ⌨️ *TES MENGETIK* 」\n"+
		"│ ⭔ `!typing easy|normal|hard` _(Buka sesi + level)_\n"+
		"│ ⭔ `!level easy|normal|hard` _(Ganti level, host)_\n"+
		"│ ⭔ `!join` _(Ikut sesi yang sedang dibuka)_\n"+
		"│ ⭔ `!mulai` _(Mulai tes, khusus host)_\n"+
		"│ ⭔ `!batal` _(Batalkan sesi, host)_\n"+
		"│ ⭔ `!leaderboard` _(Lihat skor terakhir)_\n"+
		"╰────────────────────────┈\n\n"+
		"╭─「 ⚡ *SISTEM & UTILITAS* 」\n"+
		"│ ⭔ `!ping` _(Cek status & latensi bot)_\n"+
		"│ ⭔ `!menu` _(Tampilkan menu dashboard ini)_\n"+
		"╰────────────────────────┈\n\n"+
		"╭─「 💬 *OBROLAN & CURHAT KULIAH* 」\n"+
		"│ Lisa dibekali kecerdasan natural, tanyakan apa saja:\n"+
		"│ ⭔ _\"kalau besok ga turun dosen marah ga?\"_\n"+
		"│ ⭔ _\"rekomendasi judul skripsi hukum\"_\n"+
		"│ ⭔ _\"jelaskan apa itu hukum perdata\"_\n"+
		"╰────────────────────────┈\n\n"+
		"✨ _AI Lisa Assistant • Cepat, Ringan & Otomatis_",
		userName, greeting, userName, userRole, uptime, adminSection)

	return menu, nil
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%02dj %02dm %02dd", h, m, s)
	}
	return fmt.Sprintf("%02dm %02dd", m, s)
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

	// 3. Fallback ke Otak AI cerdas jika diaktifkan
	if h.llm != nil {
		fmt.Printf("🤖 [AI Lisa Brain] Memproses pertanyaan bebas: %q...\n", ctx.RawMessage)
		llmPrompt := fmt.Sprintf("Pengguna bertanya: %q (teks normalisasi: %q). Jawablah pertanyaan ini sebagai AI Lisa, asisten perkuliahan.", ctx.RawMessage, topicClean)
		llmResp, err := h.llm.GenerateResponse(ctx.Context, llmPrompt)
		if err == nil && strings.TrimSpace(llmResp) != "" {
			fmt.Println("✨ [AI Lisa Brain] Berhasil menghasilkan respons cerdas.")
			return llmResp, nil
		}
		fmt.Printf("⚠️ [AI Lisa Brain Error]: %v\n", err)
	}

	return fmt.Sprintf("Maaf, informasi mengenai *%s* belum ada di database. Kamu bisa menambahkannya dengan perintah:\n`@Bot tambah info %s | <isi informasi>`", topic, topic), nil
}
