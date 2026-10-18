package schedule

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"whatsapp-bot/internal/database"
	"whatsapp-bot/internal/nlu"
	"whatsapp-bot/internal/router"
)

func TestScheduleModule(t *testing.T) {
	dbPath := "test_schedule.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	db, err := database.NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := database.SeedInitialData(ctx, db); err != nil {
		t.Fatalf("failed to seed initial data: %v", err)
	}

	sc := nlu.NewSpellChecker()
	if err := database.HarvestVocabulary(ctx, db, sc); err != nil {
		t.Fatalf("failed to harvest vocabulary: %v", err)
	}

	loc, _ := time.LoadLocation("Asia/Makassar")
	engine := nlu.NewEngine(sc, loc, nil)
	handler := NewHandler(db, sc, loc)

	r := router.NewRouter()
	r.Register(handler)

	// Test 1: Query Jadwal Kamis
	t.Run("Query Jadwal Kamis with Gap Detection", func(t *testing.T) {
		query := "jadwal kamis"
		res, err := engine.Process(ctx, query)
		if err != nil {
			t.Fatalf("engine error: %v", err)
		}

		msgCtx := &router.MessageContext{
			Context:   ctx,
			SenderJID: types.NewJID("62812345678", types.DefaultUserServer),
			NLUResult: res,
		}

		reply, err := r.Route(msgCtx)
		if err != nil {
			t.Fatalf("router error: %v", err)
		}

		if !strings.Contains(reply, "Jadwal Hari Kamis") {
			t.Errorf("expected header 'Jadwal Hari Kamis', got: %s", reply)
		}
		if !strings.Contains(reply, "Pend. Bahasa Indonesia") {
			t.Errorf("expected 'Pend. Bahasa Indonesia', got: %s", reply)
		}
		if !strings.Contains(reply, "jeda kosong 2 jam 10 menit") {
			t.Errorf("expected gap calculator detection 'jeda kosong 2 jam 10 menit', got: %s", reply)
		}
		t.Logf("Reply:\n%s", reply)
	})

	// Test 2: Reverse Subject Lookup (PIH)
	t.Run("Reverse Subject Lookup PIH", func(t *testing.T) {
		query := "kapan kelas pih?"
		res, err := engine.Process(ctx, query)
		if err != nil {
			t.Fatalf("engine error: %v", err)
		}

		msgCtx := &router.MessageContext{
			Context:   ctx,
			SenderJID: types.NewJID("62812345678", types.DefaultUserServer),
			NLUResult: res,
		}

		reply, err := r.Route(msgCtx)
		if err != nil {
			t.Fatalf("router error: %v", err)
		}

		if !strings.Contains(reply, "Pengantar Ilmu Hukum") {
			t.Errorf("expected 'Pengantar Ilmu Hukum', got: %s", reply)
		}
		t.Logf("Reply:\n%s", reply)
	})

	// Test 3: Schedule Override
	t.Run("Schedule Override Recording", func(t *testing.T) {
		query := "info hari ini pih pindah zoom"
		res, err := engine.Process(ctx, query)
		if err != nil {
			t.Fatalf("engine error: %v", err)
		}

		msgCtx := &router.MessageContext{
			Context:    ctx,
			SenderJID:  types.NewJID("62812345678", types.DefaultUserServer),
			SenderName: "Budi",
			NLUResult:  res,
		}

		reply, err := r.Route(msgCtx)
		if err != nil {
			t.Fatalf("router error: %v", err)
		}

		if !strings.Contains(reply, "Update Jadwal Harian Tersimpan") {
			t.Errorf("expected save confirmation, got: %s", reply)
		}
		t.Logf("Reply:\n%s", reply)
	})
}
