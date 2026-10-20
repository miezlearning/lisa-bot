package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"whatsapp-bot/internal/database"
	"whatsapp-bot/internal/modules/admin"
	"whatsapp-bot/internal/modules/knowledge"
	"whatsapp-bot/internal/modules/schedule"
	"whatsapp-bot/internal/modules/typing"
	"whatsapp-bot/internal/nlu"
	"whatsapp-bot/internal/router"
	"whatsapp-bot/internal/whatsapp"
)

// loadEnv reads a simple key=value .env file if it exists and sets environment variables.
func loadEnv(filepath string) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
	}
}

func main() {
	loadEnv(".env")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("==================================================")
	fmt.Println("🚀 Memulai AI Lisa - WhatsApp Assistant Kuliah...")
	fmt.Println("==================================================")

	// 1. Konfigurasi Timezone
	tzName := os.Getenv("TIMEZONE")
	if tzName == "" {
		tzName = "Asia/Makassar"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		log.Printf("⚠️ Gagal memuat timezone %s, beralih ke Asia/Jakarta: %v", tzName, err)
		loc, err = time.LoadLocation("Asia/Jakarta")
		if err != nil {
			loc = time.Local
		}
	}
	fmt.Printf("🕒 Timezone aktif: %s\n", loc.String())

	// 2. Inisialisasi Database SQLite & Auto-Migration
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "bot.db"
	}
	db, err := database.NewDB(dbPath)
	if err != nil {
		log.Fatalf("❌ Gagal inisialisasi database SQLite: %v", err)
	}
	defer db.Close()
	fmt.Println("📦 Database SQLite & Auto-Migration siap.")

	// 3. Seeding Data Awal Jadwal & Knowledge Base
	if err := database.SeedInitialData(ctx, db); err != nil {
		log.Fatalf("❌ Gagal seeding data awal: %v", err)
	}
	fmt.Println("🌱 Data awal jadwal & aturan berhasil diverifikasi/seeded.")

	// Seeding Admin Numbers jika ada di ENV (e.g. ADMIN_NUMBERS=62812345678,62898765432)
	adminEnv := os.Getenv("ADMIN_NUMBERS")
	if adminEnv != "" {
		adminList := strings.Split(adminEnv, ",")
		_ = database.SeedInitialAdmins(ctx, db, adminList)
		fmt.Printf("👑 Seeding %d admin dari environment (.env).\n", len(adminList))
	}

	adminCount, _ := db.GetAdminCount(ctx)
	if adminCount == 0 {
		fmt.Println("⚠️ PERHATIAN: Belum ada admin terdaftar di database.")
		fmt.Println("   Ketik '@Bot izinkan grup' di grup atau chat untuk klaim admin pertama.")
	} else {
		fmt.Printf("👑 Terdaftar %d admin di database.\n", adminCount)
	}

	// 4. Inisialisasi QWERTY SpellChecker & Vocabulary Harvester
	spellchecker := nlu.NewSpellChecker()
	if err := database.HarvestVocabulary(ctx, db, spellchecker); err != nil {
		log.Printf("⚠️ Peringatan: harvest vocabulary menemui kendala: %v", err)
	} else {
		fmt.Println("🧠 Dynamic Vocabulary & Course Acronyms berhasil di-harvest.")
	}

	// 5. Inisialisasi NLU Engine & Optional LLM Fallback (Google Gemini Free)
	var llmFallback nlu.LLMFallback
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey != "" {
		llmFallback = nlu.NewGeminiClient(geminiKey)
		fmt.Println("✨ Otak AI Lisa aktif untuk percakapan cerdas & asisten pintar.")
	}
	engine := nlu.NewEngine(spellchecker, loc, llmFallback)

	// 6. Inisialisasi Registry Router & Modul Domain
	botRouter := router.NewRouter()

	schedHandler := schedule.NewHandler(db, spellchecker, loc)
	botRouter.Register(schedHandler)

	knowHandler := knowledge.NewHandler(db, spellchecker, llmFallback)
	botRouter.Register(knowHandler)

	adminHandler := admin.NewHandler(db)
	botRouter.Register(adminHandler)

	typingManager := typing.NewManager()

	fmt.Println("🧩 Modul Schedule, Knowledge Base, Admin, dan Typing Test berhasil diregistrasi.")

	// 7. Inisialisasi Whatsmeow WhatsApp Client
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "INFO"
	}

	waClient, err := whatsapp.NewClient(ctx, dbPath, logLevel)
	if err != nil {
		log.Fatalf("❌ Gagal membuat WhatsApp client: %v", err)
	}

	// 8. Registrasi Event Handler (JID/LID Mention Aware + Group Whitelist Restrictor)
	eventHandler := whatsapp.NewEventHandler(waClient.WAClient, db, engine, botRouter, typingManager)
	typingManager.SetSender(eventHandler.SendText)
	eventHandler.Register()

	// 9. Connect WhatsApp Client
	if err := waClient.Connect(ctx); err != nil {
		log.Fatalf("❌ Gagal menghubungkan WhatsApp client: %v", err)
	}

	fmt.Println("🤖 Bot Lisa aktif dan siap melayani pesan WhatsApp!")
	fmt.Println("Tekan Ctrl+C di terminal untuk mematikan bot secara graceful.")

	// 10. Graceful Shutdown Listener
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	<-stopChan
	fmt.Println("\n🛑 Menerima sinyal shutdown, menutup koneksi...")

	waClient.Disconnect()
	fmt.Println("✅ AI Lisa WhatsApp Bot berhenti dengan aman.")
}
