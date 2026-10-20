package reminder

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"whatsapp-bot/internal/database"
	"whatsapp-bot/internal/nlu"
	"whatsapp-bot/internal/router"
)

func setupTestDB(t *testing.T) *database.DB {
	t.Helper()

	dbPath := "test_reminder_" + strings.ReplaceAll(t.Name(), "/", "_") + ".db"
	_ = os.Remove(dbPath)
	t.Cleanup(func() { _ = os.Remove(dbPath) })

	db, err := database.NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestRouter(t *testing.T, db *database.DB, loc *time.Location) (*nlu.Engine, *router.Router, *Handler) {
	t.Helper()

	sc := nlu.NewSpellChecker()
	engine := nlu.NewEngine(sc, loc, nil)
	handler := NewHandler(db, loc)

	r := router.NewRouter()
	r.Register(handler)
	return engine, r, handler
}

func routeMessage(t *testing.T, engine *nlu.Engine, r *router.Router, ctx context.Context, sender, chat types.JID, text string) string {
	t.Helper()

	result, err := engine.Process(ctx, text)
	if err != nil {
		t.Fatalf("engine error for %q: %v", text, err)
	}
	reply, err := r.Route(&router.MessageContext{
		Context:      ctx,
		SenderJID:    sender,
		SenderPhone:  sender.User,
		SenderName:   "Tester",
		ChatJID:      chat,
		RawMessage:   text,
		CleanMessage: text,
		NLUResult:    result,
	})
	if err != nil {
		t.Fatalf("router error for %q: %v", text, err)
	}
	return reply
}

func TestReminderHandlerFlow(t *testing.T) {
	db := setupTestDB(t)
	loc := mustLocation(t)
	engine, r, _ := newTestRouter(t, db, loc)

	ctx := context.Background()
	sender := types.NewJID("62812345678", types.DefaultUserServer)
	chat := types.NewJID("62811111111", types.DefaultUserServer)

	reply := routeMessage(t, engine, r, ctx, sender, chat, "ingatkan 2 jam lagi minum obat")
	if !strings.Contains(reply, "Pengingat Tersimpan") {
		t.Fatalf("expected save confirmation, got: %s", reply)
	}

	items, err := db.GetActiveReminders(ctx, chat.String(), 10)
	if err != nil {
		t.Fatalf("GetActiveReminders error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 active reminder, got %d", len(items))
	}
	if items[0].Message != "minum obat" {
		t.Errorf("message = %q, want %q", items[0].Message, "minum obat")
	}
	if items[0].SenderJID != sender.String() {
		t.Errorf("sender = %q, want %q", items[0].SenderJID, sender.String())
	}

	reply = routeMessage(t, engine, r, ctx, sender, chat, "ingatkan")
	if !strings.Contains(reply, "minum obat") {
		t.Fatalf("expected reminder list to mention message, got: %s", reply)
	}

	reply = routeMessage(t, engine, r, ctx, sender, chat, "ingatkan hapus "+strconv.FormatInt(items[0].ID, 10))
	if !strings.Contains(reply, "berhasil dihapus") {
		t.Fatalf("expected delete confirmation, got: %s", reply)
	}

	items, err = db.GetActiveReminders(ctx, chat.String(), 10)
	if err != nil {
		t.Fatalf("GetActiveReminders error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 active reminders after delete, got %d", len(items))
	}
}

func TestReminderDeletePermissions(t *testing.T) {
	db := setupTestDB(t)
	loc := mustLocation(t)
	engine, r, _ := newTestRouter(t, db, loc)

	ctx := context.Background()
	creator := types.NewJID("62812345678", types.DefaultUserServer)
	other := types.NewJID("62899999999", types.DefaultUserServer)
	chat := types.NewJID("62811111111", types.DefaultUserServer)

	routeMessage(t, engine, r, ctx, creator, chat, "ingatkan 2 jam lagi belajar")

	items, err := db.GetActiveReminders(ctx, chat.String(), 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected 1 reminder, got %d (err=%v)", len(items), err)
	}

	reply := routeMessage(t, engine, r, ctx, other, chat, "ingatkan hapus "+strconv.FormatInt(items[0].ID, 10))
	if !strings.Contains(reply, "Hanya pembuat") {
		t.Fatalf("expected permission denial, got: %s", reply)
	}
}

func TestBriefingSettingsFlow(t *testing.T) {
	db := setupTestDB(t)
	loc := mustLocation(t)
	engine, r, _ := newTestRouter(t, db, loc)

	ctx := context.Background()
	sender := types.NewJID("62812345678", types.DefaultUserServer)
	chat := types.NewJID("62811111111", types.DefaultUserServer)

	reply := routeMessage(t, engine, r, ctx, sender, chat, "briefing")
	if !strings.Contains(reply, "Nonaktif") {
		t.Fatalf("expected inactive status, got: %s", reply)
	}

	reply = routeMessage(t, engine, r, ctx, sender, chat, "briefing on")
	if !strings.Contains(reply, "Aktif") {
		t.Fatalf("expected activation confirmation, got: %s", reply)
	}

	settings, err := db.GetBriefing(ctx, chat.String())
	if err != nil || settings == nil {
		t.Fatalf("expected briefing settings, got %v (err=%v)", settings, err)
	}
	if !settings.Enabled || settings.TimeHM != defaultBriefingTime {
		t.Fatalf("unexpected settings: %+v", settings)
	}

	routeMessage(t, engine, r, ctx, sender, chat, "briefing 05:15")
	settings, _ = db.GetBriefing(ctx, chat.String())
	if settings.TimeHM != "05:15" {
		t.Fatalf("time_hm = %q, want 05:15", settings.TimeHM)
	}

	routeMessage(t, engine, r, ctx, sender, chat, "briefing off")
	settings, _ = db.GetBriefing(ctx, chat.String())
	if settings.Enabled {
		t.Fatalf("expected briefing disabled")
	}

	reply = routeMessage(t, engine, r, ctx, sender, chat, "briefing test")
	if !strings.Contains(reply, "SELAMAT PAGI") {
		t.Fatalf("expected briefing preview, got: %s", reply)
	}
}

func TestSchedulerFiresReminderAndBriefing(t *testing.T) {
	db := setupTestDB(t)
	loc := mustLocation(t)
	ctx := context.Background()

	chatJID := "62811111111@s.whatsapp.net"
	now := time.Date(2026, 10, 19, 7, 0, 0, 0, loc)

	if _, err := db.AddReminder(ctx, database.Reminder{
		ChatJID:    chatJID,
		SenderJID:  "62812345678@s.whatsapp.net",
		SenderName: "Tester",
		Kind:       string(KindReminder),
		Message:    "minum obat",
		DueAt:      now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("AddReminder error: %v", err)
	}

	if err := db.UpsertBriefing(ctx, chatJID, "06:30", true); err != nil {
		t.Fatalf("UpsertBriefing error: %v", err)
	}

	handler := NewHandler(db, loc)

	var mu sync.Mutex
	var sent []string
	scheduler := NewScheduler(db, loc, handler.BuildBriefing, func(_ string, text string) {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, text)
	})
	scheduler.now = func() time.Time { return now }
	scheduler.tick()

	mu.Lock()
	firstBatch := append([]string(nil), sent...)
	mu.Unlock()

	if len(firstBatch) != 2 {
		t.Fatalf("expected 2 messages sent, got %d: %v", len(firstBatch), firstBatch)
	}

	joined := strings.Join(firstBatch, "\n---\n")
	if !strings.Contains(joined, "PENGINGAT") {
		t.Errorf("expected reminder notification, got: %s", joined)
	}
	if !strings.Contains(joined, "SELAMAT PAGI") {
		t.Errorf("expected briefing message, got: %s", joined)
	}

	due, err := db.GetDueReminders(ctx, now, 10)
	if err != nil {
		t.Fatalf("GetDueReminders error: %v", err)
	}
	if len(due) != 0 {
		t.Errorf("expected reminder to be marked fired, got %d pending", len(due))
	}

	settings, err := db.GetBriefing(ctx, chatJID)
	if err != nil || settings == nil {
		t.Fatalf("expected briefing settings, got %v (err=%v)", settings, err)
	}
	if settings.LastSentDate != "2026-10-19" {
		t.Errorf("last_sent_date = %q, want 2026-10-19", settings.LastSentDate)
	}

	scheduler.tick()

	mu.Lock()
	totalSent := len(sent)
	mu.Unlock()
	if totalSent != 2 {
		t.Fatalf("expected no duplicate sends on second tick, got %d messages", totalSent)
	}
}
