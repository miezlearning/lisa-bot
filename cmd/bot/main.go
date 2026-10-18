package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"whatsapp-bot/internal/database"
	"whatsapp-bot/internal/modules/knowledge"
	"whatsapp-bot/internal/modules/schedule"
	"whatsapp-bot/internal/nlu"
	"whatsapp-bot/internal/router"
	"whatsapp-bot/internal/whatsapp"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("==================================================")
	fmt.Println("🚀 Memulai AI Lisa - WhatsApp Assistant Kuliah...")
	fmt.Println("==================================================")

	// 1. Load Timezone Configuration
	tzName := os.Getenv("TIMEZONE")
	if tzName == "" {
		tzName = "Asia/Makassar" // Default timezone
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

	// 2. Initialize Database & Run Auto-Migration
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

	// 3. Seed Initial Schedules & Knowledge Base
	if err := database.SeedInitialData(ctx, db); err != nil {
		log.Fatalf("❌ Gagal seeding data awal: %v", err)
	}
	fmt.Println("🌱 Data awal jadwal & aturan berhasil diverifikasi/seeded.")

	// 4. Initialize QWERTY-Aware SpellChecker & Vocabulary Harvester
	spellchecker := nlu.NewSpellChecker()
	if err := database.HarvestVocabulary(ctx, db, spellchecker); err != nil {
		log.Printf("⚠️ Peringatan: harvest vocabulary menemui kendala: %v", err)
	} else {
		fmt.Println("🧠 Dynamic Vocabulary & Course Acronyms berhasil di-harvest.")
	}

	// 5. Initialize Deterministic NLU Engine
	engine := nlu.NewEngine(spellchecker, loc, nil)

	// 6. Initialize Router Registry & Register Domain Modules
	botRouter := router.NewRouter()

	schedHandler := schedule.NewHandler(db, spellchecker, loc)
	botRouter.Register(schedHandler)

	knowHandler := knowledge.NewHandler(db, spellchecker)
	botRouter.Register(knowHandler)

	fmt.Println("🧩 Modul Schedule dan Knowledge Base berhasil diregistrasi.")

	// 7. Initialize WhatsApp Client
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "INFO"
	}

	waClient, err := whatsapp.NewClient(ctx, dbPath, logLevel)
	if err != nil {
		log.Fatalf("❌ Gagal membuat WhatsApp client: %v", err)
	}

	// 8. Register Event Handler with JID/LID Mention Awareness
	eventHandler := whatsapp.NewEventHandler(waClient.WAClient, engine, botRouter)
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
