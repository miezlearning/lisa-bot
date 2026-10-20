package reminder

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"whatsapp-bot/internal/database"
	"whatsapp-bot/internal/nlu"
	"whatsapp-bot/internal/router"
)

const defaultBriefingTime = "06:30"

var weekdayNames = map[time.Weekday]string{
	time.Sunday:    "Minggu",
	time.Monday:    "Senin",
	time.Tuesday:   "Selasa",
	time.Wednesday: "Rabu",
	time.Thursday:  "Kamis",
	time.Friday:    "Jumat",
	time.Saturday:  "Sabtu",
}

// Handler manages reminders, deadlines, and morning briefing settings.
type Handler struct {
	db       *database.DB
	timezone *time.Location
}

// NewHandler creates a new reminder module handler.
func NewHandler(db *database.DB, loc *time.Location) *Handler {
	if loc == nil {
		loc = time.Local
	}
	return &Handler{
		db:       db,
		timezone: loc,
	}
}

func (h *Handler) Name() string {
	return "reminder"
}

func (h *Handler) CanHandle(result *nlu.NLUResult) bool {
	if result == nil {
		return false
	}
	return result.Intent == nlu.IntentReminderCommand || result.Intent == nlu.IntentBriefingCommand
}

func (h *Handler) Execute(ctx *router.MessageContext) (string, error) {
	switch ctx.NLUResult.Intent {
	case nlu.IntentBriefingCommand:
		return h.handleBriefing(ctx)
	case nlu.IntentReminderCommand:
		return h.handleReminder(ctx)
	default:
		return "Perintah pengingat tidak dikenali.", nil
	}
}

func (h *Handler) handleReminder(ctx *router.MessageContext) (string, error) {
	payload := strings.TrimSpace(ctx.NLUResult.Entities.RawPayload)
	lower := strings.ToLower(payload)
	chatJID := ctx.ChatJID.String()

	switch {
	case payload == "" || lower == "daftar" || lower == "list" || lower == "semua" || lower == "status":
		return h.listReminders(ctx, chatJID)
	case strings.HasPrefix(lower, "hapus ") || strings.HasPrefix(lower, "delete ") ||
		strings.HasPrefix(lower, "batalkan ") || strings.HasPrefix(lower, "cancel ") ||
		strings.HasPrefix(lower, "selesai "):
		return h.deleteReminder(ctx, payload, chatJID)
	}

	kind := KindReminder
	if ctx.NLUResult.Entities.Command == "deadline" {
		kind = KindDeadline
	}

	now := time.Now().In(h.timezone)
	parsed, err := ParseWhen(payload, now, kind)
	if err != nil {
		return "⚠️ " + err.Error(), nil
	}

	sender := ctx.SenderName
	if sender == "" {
		sender = ctx.SenderJID.User
	}

	id, err := h.db.AddReminder(ctx.Context, database.Reminder{
		ChatJID:    chatJID,
		SenderJID:  ctx.SenderJID.String(),
		SenderName: sender,
		Kind:       string(kind),
		Message:    parsed.Message,
		DueAt:      parsed.Due,
	})
	if err != nil {
		return "", fmt.Errorf("gagal menyimpan pengingat: %w", err)
	}

	icon := "⏰"
	label := "Pengingat"
	if kind == KindDeadline {
		icon = "📌"
		label = "Deadline"
	}

	return fmt.Sprintf("%s *%s Tersimpan!* (ID: %d)\n\n📅 *%s*\n📝 %s\n⏳ %s lagi\n\n_Ketik `!ingatkan hapus %d` untuk membatalkan._",
		icon, label, id, formatDueLabel(now, parsed.Due, h.timezone), parsed.Message,
		formatCountdown(parsed.Due.Sub(now)), id), nil
}

func (h *Handler) listReminders(ctx *router.MessageContext, chatJID string) (string, error) {
	items, err := h.db.GetActiveReminders(ctx.Context, chatJID, 20)
	if err != nil {
		return "", fmt.Errorf("gagal mengambil daftar pengingat: %w", err)
	}
	if len(items) == 0 {
		return "⏰ *Belum ada pengingat aktif di chat ini.*\n\nBuat dengan:\n• `!ingatkan besok jam 8 kumpul tugas`\n• `!deadline 25/10 23:59 tugas hukum`", nil
	}

	now := time.Now().In(h.timezone)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("⏰ *Pengingat Aktif* (%d)\n\n", len(items)))
	for _, item := range items {
		icon := "⏰"
		if item.Kind == string(KindDeadline) {
			icon = "📌"
		}
		sb.WriteString(fmt.Sprintf("%s *#%d* — %s\n   📅 %s\n\n", icon, item.ID, item.Message, formatDueLabel(now, item.DueAt, h.timezone)))
	}
	sb.WriteString("_Hapus dengan `!ingatkan hapus <id>`._")
	return strings.TrimSpace(sb.String()), nil
}

func (h *Handler) deleteReminder(ctx *router.MessageContext, payload, chatJID string) (string, error) {
	fields := strings.Fields(payload)
	if len(fields) < 2 {
		return "⚠️ Format: `!ingatkan hapus <id>`", nil
	}

	id, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
	if err != nil || id <= 0 {
		return "⚠️ ID pengingat tidak valid. Contoh: `!ingatkan hapus 12`", nil
	}

	item, err := h.db.GetReminder(ctx.Context, id)
	if err != nil {
		return "", fmt.Errorf("gagal mengambil pengingat: %w", err)
	}
	if item == nil || item.ChatJID != chatJID {
		return fmt.Sprintf("⚠️ Pengingat #%d tidak ditemukan di chat ini.", id), nil
	}
	if !h.canManage(ctx, item) {
		return "⛔ Hanya pembuat pengingat atau admin yang bisa menghapusnya.", nil
	}

	if err := h.db.DeleteReminder(ctx.Context, id); err != nil {
		return "", fmt.Errorf("gagal menghapus pengingat: %w", err)
	}
	return fmt.Sprintf("🗑️ Pengingat #%d (*%s*) berhasil dihapus.", id, item.Message), nil
}

func (h *Handler) handleBriefing(ctx *router.MessageContext) (string, error) {
	payload := strings.TrimSpace(strings.ToLower(ctx.NLUResult.Entities.RawPayload))
	chatJID := ctx.ChatJID.String()

	existing, err := h.db.GetBriefing(ctx.Context, chatJID)
	if err != nil {
		return "", fmt.Errorf("gagal mengambil pengaturan briefing: %w", err)
	}

	switch {
	case payload == "" || payload == "status" || payload == "info":
		return h.briefingStatus(existing), nil

	case payload == "on" || payload == "aktif" || payload == "aktifkan" || payload == "nyala" || payload == "nyalakan" || payload == "enable":
		if !h.authorizeBriefing(ctx) {
			return "⛔ Pengaturan briefing di grup hanya bisa diubah oleh admin.", nil
		}
		hm := h.currentBriefingTime(existing)
		if err := h.db.UpsertBriefing(ctx.Context, chatJID, hm, true); err != nil {
			return "", fmt.Errorf("gagal mengaktifkan briefing: %w", err)
		}
		return fmt.Sprintf("🌅 *Morning Briefing Aktif!*\n\nJam kirim: *%s* (%s)\nLisa akan mengirim jadwal hari ini, deadline terdekat, dan pengingat setiap pagi.\n\n_Ubah jam: `!briefing 06:00` • Matikan: `!briefing off`_", hm, h.timezone.String()), nil

	case payload == "off" || payload == "nonaktif" || payload == "matikan" || payload == "mati" || payload == "disable":
		if !h.authorizeBriefing(ctx) {
			return "⛔ Pengaturan briefing di grup hanya bisa diubah oleh admin.", nil
		}
		hm := h.currentBriefingTime(existing)
		if err := h.db.UpsertBriefing(ctx.Context, chatJID, hm, false); err != nil {
			return "", fmt.Errorf("gagal menonaktifkan briefing: %w", err)
		}
		return "🌙 *Morning Briefing Dinonaktifkan.*\n\nAktifkan lagi kapan saja dengan `!briefing on`.", nil

	case payload == "test" || payload == "preview" || payload == "contoh" || payload == "sekarang":
		if !h.authorizeBriefing(ctx) {
			return "⛔ Pratinjau briefing di grup hanya bisa diminta oleh admin.", nil
		}
		return h.BuildBriefing(ctx.Context, chatJID, time.Now().In(h.timezone)), nil
	}

	hmPayload := strings.TrimSpace(strings.TrimPrefix(payload, "jam"))
	hour, minute, ok := parseTimeHM(hmPayload)
	if !ok {
		return "⚠️ Format jam tidak valid. Contoh:\n• `!briefing on`\n• `!briefing off`\n• `!briefing 06:30`\n• `!briefing test`", nil
	}
	if !h.authorizeBriefing(ctx) {
		return "⛔ Pengaturan briefing di grup hanya bisa diubah oleh admin.", nil
	}

	hm := fmt.Sprintf("%02d:%02d", hour, minute)
	if err := h.db.UpsertBriefing(ctx.Context, chatJID, hm, true); err != nil {
		return "", fmt.Errorf("gagal mengatur jam briefing: %w", err)
	}
	return fmt.Sprintf("🌅 *Jam Morning Briefing Diatur!*\n\n⏰ Setiap hari pukul *%s* (%s).\n\n_Matikan: `!briefing off` • Pratinjau: `!briefing test`_", hm, h.timezone.String()), nil
}

func (h *Handler) briefingStatus(existing *database.Briefing) string {
	status := "Nonaktif"
	hm := defaultBriefingTime
	if existing != nil {
		hm = existing.TimeHM
		if existing.Enabled {
			status = "Aktif"
		}
	}

	return fmt.Sprintf("🌅 *Morning Briefing*\n\n📊 Status: *%s*\n🕒 Jam kirim: *%s* (%s)\n\nPerintah:\n• `!briefing on` — aktifkan\n• `!briefing off` — matikan\n• `!briefing 06:30` — ubah jam\n• `!briefing test` — pratinjau sekarang",
		status, hm, h.timezone.String())
}

func (h *Handler) currentBriefingTime(existing *database.Briefing) string {
	if existing != nil && strings.TrimSpace(existing.TimeHM) != "" {
		return existing.TimeHM
	}
	return defaultBriefingTime
}

func (h *Handler) authorizeBriefing(ctx *router.MessageContext) bool {
	if !ctx.IsGroup {
		return true
	}
	return h.isAdmin(ctx)
}

func (h *Handler) isAdmin(ctx *router.MessageContext) bool {
	phone := ctx.SenderPhone
	if phone == "" {
		phone = ctx.SenderJID.User
	}
	return h.db.IsAdmin(ctx.Context, phone) || h.db.IsAdmin(ctx.Context, ctx.SenderJID.User)
}

func (h *Handler) canManage(ctx *router.MessageContext, item *database.Reminder) bool {
	if h.isAdmin(ctx) {
		return true
	}
	sender := ctx.SenderJID.ToNonAD().String()
	return item.SenderJID == sender || item.SenderJID == ctx.SenderJID.String()
}

// BuildBriefing composes the morning briefing message for a chat.
func (h *Handler) BuildBriefing(ctx context.Context, chatJID string, now time.Time) string {
	now = now.In(h.timezone)
	day := nlu.GoWeekdayToScheduleDay(now.Weekday())

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🌅 *SELAMAT PAGI!*\n📅 %s, %s\n\n", nlu.IntToDayName[day], now.Format("02 Jan 2006")))

	schedules, err := h.db.GetSchedulesByDay(ctx, day)
	if err == nil {
		if len(schedules) == 0 {
			sb.WriteString("🎉 *Tidak ada kelas hari ini.* Selamat beristirahat!\n")
		} else {
			sb.WriteString("📚 *Jadwal Hari Ini*\n")
			overrides := make(map[string]string)
			if list, err := h.db.GetOverridesByDate(ctx, now.Format("2006-01-02")); err == nil {
				for _, o := range list {
					overrides[strings.ToLower(o.Subject)] = o.Note
				}
			}
			for _, s := range schedules {
				if s.StartTime != "" && s.EndTime != "" {
					sb.WriteString(fmt.Sprintf("• *%s–%s* %s", strings.ReplaceAll(s.StartTime, ":", "."), strings.ReplaceAll(s.EndTime, ":", "."), s.Subject))
				} else {
					sb.WriteString("• " + s.Subject)
				}
				if s.Room != "" {
					sb.WriteString(" (" + s.Room + ")")
				}
				if note, ok := overrides[strings.ToLower(s.Subject)]; ok && note != "" {
					sb.WriteString(" ⚠️ " + note)
				}
				sb.WriteString("\n")
			}
		}
	}

	deadlines, err := h.db.GetUpcomingDeadlines(ctx, chatJID, now, now.AddDate(0, 0, 7))
	if err == nil && len(deadlines) > 0 {
		sb.WriteString("\n📌 *Deadline 7 Hari ke Depan*\n")
		for _, d := range deadlines {
			sb.WriteString(fmt.Sprintf("• %s — %s\n", relativeDayLabel(now, d.DueAt, h.timezone), d.Message))
		}
	} else {
		sb.WriteString("\n📌 *Deadline:* tidak ada dalam 7 hari ke depan.\n")
	}

	reminders, err := h.db.GetActiveReminders(ctx, chatJID, 50)
	if err == nil {
		var today []database.Reminder
		for _, r := range reminders {
			if r.Kind == string(KindReminder) && sameDate(r.DueAt.In(h.timezone), now) {
				today = append(today, r)
			}
		}
		if len(today) > 0 {
			sb.WriteString("\n⏰ *Pengingat Hari Ini*\n")
			for _, r := range today {
				sb.WriteString(fmt.Sprintf("• %s — %s\n", r.DueAt.In(h.timezone).Format("15:04"), r.Message))
			}
		}
	}

	sb.WriteString("\n✨ _Semangat kuliah hari ini!_")
	return sb.String()
}

func formatDueLabel(now, due time.Time, loc *time.Location) string {
	due = due.In(loc)
	prefix := weekdayNames[due.Weekday()]
	switch daysBetween(now.In(loc), due) {
	case 0:
		prefix = "Hari ini"
	case 1:
		prefix = "Besok"
	case 2:
		prefix = "Lusa"
	}
	return fmt.Sprintf("%s, %s • %s", prefix, due.Format("02 Jan 2006"), due.Format("15:04"))
}

func relativeDayLabel(now, due time.Time, loc *time.Location) string {
	due = due.In(loc)
	prefix := due.Format("02 Jan")
	switch daysBetween(now.In(loc), due) {
	case 0:
		prefix = "Hari ini"
	case 1:
		prefix = "Besok"
	case 2:
		prefix = "Lusa"
	}
	return fmt.Sprintf("%s %s", prefix, due.Format("15:04"))
}

func formatCountdown(d time.Duration) string {
	if d <= 0 {
		return "kurang dari 1 menit"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	switch {
	case days > 0:
		return fmt.Sprintf("%d hari %d jam", days, hours)
	case hours > 0:
		return fmt.Sprintf("%d jam %d menit", hours, minutes)
	default:
		return fmt.Sprintf("%d menit", minutes)
	}
}

func daysBetween(from, to time.Time) int {
	fromDate := startOfDay(from)
	toDate := startOfDay(to)
	return int(toDate.Sub(fromDate).Hours() / 24)
}
