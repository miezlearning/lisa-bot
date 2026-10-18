package admin

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

func TestAdminModule(t *testing.T) {
	dbPath := "test_admin.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	db, err := database.NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Add super admin
	adminPhone := "62811111111"
	if err := db.AddAdmin(ctx, adminPhone, "Super Admin", "system"); err != nil {
		t.Fatalf("failed to add admin: %v", err)
	}

	sc := nlu.NewSpellChecker()
	loc, _ := time.LoadLocation("Asia/Makassar")
	engine := nlu.NewEngine(sc, loc, nil)
	adminHandler := NewHandler(db)

	r := router.NewRouter()
	r.Register(adminHandler)

	// Test 1: Non-admin rejected
	t.Run("Non-admin rejection", func(t *testing.T) {
		res, _ := engine.Process(ctx, "izinkan grup")
		msgCtx := &router.MessageContext{
			Context:   ctx,
			SenderJID: types.NewJID("62899999999", types.DefaultUserServer),
			ChatJID:   types.NewJID("120363000@g.us", types.GroupServer),
			IsGroup:   true,
			NLUResult: res,
		}

		reply, err := r.Route(msgCtx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(reply, "Akses Ditolak") {
			t.Errorf("expected access denied for non-admin, got: %s", reply)
		}
	})

	// Test 2: Admin can allow group
	t.Run("Admin allow group", func(t *testing.T) {
		groupJID := types.NewJID("120363000@g.us", types.GroupServer)
		res, _ := engine.Process(ctx, "izinkan grup")
		msgCtx := &router.MessageContext{
			Context:   ctx,
			SenderJID: types.NewJID(adminPhone, types.DefaultUserServer),
			ChatJID:   groupJID,
			IsGroup:   true,
			NLUResult: res,
		}

		reply, err := r.Route(msgCtx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(reply, "Grup Berhasil Diizinkan") {
			t.Errorf("expected group allowed confirmation, got: %s", reply)
		}

		// Verify database state
		if !db.IsGroupAllowed(ctx, groupJID.String()) {
			t.Errorf("expected group to be allowed in database")
		}
	})

	// Test 3: List allowed groups
	t.Run("Admin list groups", func(t *testing.T) {
		res, _ := engine.Process(ctx, "daftar grup")
		msgCtx := &router.MessageContext{
			Context:   ctx,
			SenderJID: types.NewJID(adminPhone, types.DefaultUserServer),
			NLUResult: res,
		}

		reply, err := r.Route(msgCtx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(reply, "120363000@g.us") {
			t.Errorf("expected listed group in reply, got: %s", reply)
		}
	})

	// Test 4: Add new admin
	t.Run("Admin add new admin", func(t *testing.T) {
		res, _ := engine.Process(ctx, "tambah admin 08123456789")
		msgCtx := &router.MessageContext{
			Context:   ctx,
			SenderJID: types.NewJID(adminPhone, types.DefaultUserServer),
			NLUResult: res,
		}

		reply, err := r.Route(msgCtx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(reply, "Admin Berhasil Ditambahkan") {
			t.Errorf("expected add admin confirmation, got: %s", reply)
		}

		// Verify 628123456789 is recognized as admin
		if !db.IsAdmin(ctx, "628123456789") {
			t.Errorf("expected 628123456789 to be recognized as admin")
		}
	})
}
