package knowledge

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

func TestKnowledgeModule(t *testing.T) {
	dbPath := "test_knowledge.db"
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
	handler := NewHandler(db, sc)

	r := router.NewRouter()
	r.Register(handler)

	// Test 1: Query with typo corrected
	t.Run("Query Larangan with typo laranfan", func(t *testing.T) {
		query := "itu kaya laranfan praktikum apa aja?"
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

		if !strings.Contains(reply, "Larangan Praktikum") {
			t.Errorf("expected content about Larangan Praktikum, got: %s", reply)
		}
		t.Logf("Reply:\n%s", reply)
	})

	// Test 2: Add dynamic knowledge and retrieve it
	t.Run("Add and Query Dynamic Knowledge", func(t *testing.T) {
		addQuery := "tambah info beasiswa | Pendaftaran dibuka sampai tanggal 30 November."
		res, err := engine.Process(ctx, addQuery)
		if err != nil {
			t.Fatalf("engine error: %v", err)
		}

		msgCtx := &router.MessageContext{
			Context:   ctx,
			SenderJID: types.NewJID("62812345678", types.DefaultUserServer),
			NLUResult: res,
		}

		addReply, err := r.Route(msgCtx)
		if err != nil {
			t.Fatalf("router error: %v", err)
		}

		if !strings.Contains(addReply, "Pengetahuan Baru Berhasil Disimpan") {
			t.Errorf("expected success message, got: %s", addReply)
		}

		// Now query the newly added knowledge
		query := "info beasiswa"
		resQuery, err := engine.Process(ctx, query)
		if err != nil {
			t.Fatalf("engine error: %v", err)
		}

		msgCtxQuery := &router.MessageContext{
			Context:   ctx,
			SenderJID: types.NewJID("62812345678", types.DefaultUserServer),
			NLUResult: resQuery,
		}

		reply, err := r.Route(msgCtxQuery)
		if err != nil {
			t.Fatalf("router error: %v", err)
		}

		if !strings.Contains(reply, "30 November") {
			t.Errorf("expected newly added content, got: %s", reply)
		}
		t.Logf("Reply:\n%s", reply)
	})
}
