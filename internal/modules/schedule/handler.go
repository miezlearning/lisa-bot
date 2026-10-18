package schedule

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"whatsapp-bot/internal/database"
	"whatsapp-bot/internal/nlu"
	"whatsapp-bot/internal/router"
)

// Handler handles schedule inquiries, real-time status queries, crowd-sourced overrides, and raw schedule imports.
type Handler struct {
	db       *database.DB
	sc       *nlu.SpellChecker
	timezone *time.Location
}

// NewHandler creates a new schedule module handler.
func NewHandler(db *database.DB, sc *nlu.SpellChecker, loc *time.Location) *Handler {
	if loc == nil {
		loc = time.Local
	}
	return &Handler{
		db:       db,
		sc:       sc,
		timezone: loc,
	}
}

func (h *Handler) Name() string {
	return "schedule"
}

func (h *Handler) CanHandle(result *nlu.NLUResult) bool {
	if result == nil {
		return false
	}
	return result.Intent == nlu.IntentScheduleQuery ||
		result.Intent == nlu.IntentScheduleOverride ||
		result.Intent == nlu.IntentScheduleSaveRaw
}

func (h *Handler) Execute(ctx *router.MessageContext) (string, error) {
	result := ctx.NLUResult

	switch result.Intent {
	case nlu.IntentScheduleSaveRaw:
		return h.handleSaveRawSchedule(ctx)
	case nlu.IntentScheduleOverride:
		return h.handleScheduleOverride(ctx)
	case nlu.IntentScheduleQuery:
		return h.handleScheduleQuery(ctx)
	default:
		return "Perintah jadwal tidak dikenali.", nil
	}
}

func (h *Handler) handleSaveRawSchedule(ctx *router.MessageContext) (string, error) {
	payload := ctx.NLUResult.Entities.RawPayload
	if strings.TrimSpace(payload) == "" {
		return "⚠️ Format tidak lengkap. Kirim `@Bot simpan jadwal` diikuti teks jadwal mentah WhatsApp.", nil
	}

	parsed := ParseRawSchedule(payload)
	if len(parsed) == 0 {
		return "⚠️ Tidak ada data jadwal yang berhasil dikenali. Pastikan menyertakan format hari (*Senin*), jam (_07.30 - 09.10_), dan nama mata kuliah.", nil
	}

	if err := h.db.ClearAndInsertSchedules(ctx.Context, parsed); err != nil {
		return "", fmt.Errorf("failed to save schedules: %w", err)
	}

	// Re-harvest vocabulary into spellchecker
	_ = database.HarvestVocabulary(ctx.Context, h.db, h.sc)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("✅ *Jadwal Berhasil Diperbarui!*\nTotal %d mata kuliah telah tersimpan ke database:\n\n", len(parsed)))
	for _, item := range parsed {
		timeStr := ""
		if item.StartTime != "" && item.EndTime != "" {
			timeStr = fmt.Sprintf(" (%s - %s)", item.StartTime, item.EndTime)
		}
		sb.WriteString(fmt.Sprintf("• *%s*: %s%s\n", item.DayName, item.Subject, timeStr))
	}

	return sb.String(), nil
}

func (h *Handler) handleScheduleOverride(ctx *router.MessageContext) (string, error) {
	subject := ctx.NLUResult.Entities.Subject
	note := ctx.NLUResult.Entities.OverrideNote
	dateStr := ctx.NLUResult.Entities.TargetDate

	if subject == "" || note == "" {
		return "⚠️ Format belum lengkap. Gunakan contoh:\n`@Bot info hari ini pih pindah zoom`", nil
	}

	sender := ctx.SenderName
	if sender == "" {
		sender = ctx.SenderJID.User
	}

	override := database.ScheduleOverride{
		OverrideDate: dateStr,
		Subject:      subject,
		Note:         note,
		SenderJID:    ctx.SenderJID.String(),
		SenderName:   sender,
	}

	if err := h.db.AddScheduleOverride(ctx.Context, override); err != nil {
		return "", fmt.Errorf("failed to save override: %w", err)
	}

	now := time.Now().In(h.timezone)
	dateHuman := now.Format("02 Jan 2006")

	return fmt.Sprintf("✅ *Update Jadwal Harian Tersimpan!*\n\n📅 Tanggal: %s\n📘 Matkul: *%s*\n📝 Info: %s\n👤 Pengirim: %s\n\n_Catatan ini akan otomatis ditampilkan saat teman-teman mengecek jadwal hari ini._",
		dateHuman, subject, note, sender), nil
}

func (h *Handler) handleScheduleQuery(ctx *router.MessageContext) (string, error) {
	ent := ctx.NLUResult.Entities
	now := time.Now().In(h.timezone)

	// 1. Real-time Status ("sekarang kelas apa?", "habis ini masuk mana?")
	if ent.RelativeTime == nlu.RelativeNow {
		return h.handleRealTimeStatus(ctx.Context, now)
	}

	// 2. Reverse Lookup by Subject ("kapan kelas pih?", "pengantar ilmu hukum hari apa aja?")
	if ent.Subject != "" && ent.TargetDay == 0 && ent.RelativeTime == nlu.RelativeNone {
		return h.handleSubjectLookup(ctx.Context, ent.Subject)
	}

	// 3. Reverse Lookup by Room ("ruang B.204 dipakai kapan?")
	if ent.Room != "" && ent.TargetDay == 0 && ent.RelativeTime == nlu.RelativeNone {
		return h.handleRoomLookup(ctx.Context, ent.Room)
	}

	// 4. Schedule by Day (explicit day or relative day)
	targetDay := ent.TargetDay
	if targetDay == 0 {
		targetDay = nlu.GoWeekdayToScheduleDay(now.Weekday())
	}

	dayName := nlu.IntToDayName[targetDay]
	schedules, err := h.db.GetSchedulesByDay(ctx.Context, targetDay)
	if err != nil {
		return "", err
	}

	if len(schedules) == 0 {
		return fmt.Sprintf("🎉 *Tidak ada jadwal kuliah di hari %s.* Selamat beristirahat!", dayName), nil
	}

	// Fetch any overrides for target date if checking today/tomorrow
	overridesMap := make(map[string]string)
	if ent.TargetDate != "" {
		if overrides, err := h.db.GetOverridesByDate(ctx.Context, ent.TargetDate); err == nil {
			for _, o := range overrides {
				overridesMap[strings.ToLower(o.Subject)] = fmt.Sprintf("⚠️ _Update (%s): %s_", o.SenderName, o.Note)
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📅 *Jadwal Hari %s*\n\n", dayName))

	for i, s := range schedules {
		if s.StartTime != "" && s.EndTime != "" {
			sb.WriteString(fmt.Sprintf("🕒 _%s - %s_\n", strings.ReplaceAll(s.StartTime, ":", "."), strings.ReplaceAll(s.EndTime, ":", ".")))
		}
		sb.WriteString(fmt.Sprintf("📘 *%s*\n", s.Subject))
		if s.Room != "" {
			sb.WriteString(fmt.Sprintf("📍 %s\n", s.Room))
		}
		if s.Notes != "" {
			sb.WriteString(fmt.Sprintf("📝 _Catatan: %s_\n", s.Notes))
		}

		// Show crowd-sourced override note if present
		if overrideText, ok := overridesMap[strings.ToLower(s.Subject)]; ok {
			sb.WriteString(overrideText + "\n")
		}

		if i < len(schedules)-1 {
			sb.WriteString("\n")
		}
	}

	// Gap Calculator (Detect empty gap >= 60 mins between classes)
	gaps := calculateGaps(schedules)
	if len(gaps) > 0 {
		sb.WriteString("\n\n" + strings.Join(gaps, "\n"))
	}

	return sb.String(), nil
}

func (h *Handler) handleRealTimeStatus(ctx context.Context, now time.Time) (string, error) {
	currentDay := nlu.GoWeekdayToScheduleDay(now.Weekday())
	dayName := nlu.IntToDayName[currentDay]
	currentTimeStr := now.Format("15:04")

	schedules, err := h.db.GetSchedulesByDay(ctx, currentDay)
	if err != nil {
		return "", err
	}

	if len(schedules) == 0 {
		return fmt.Sprintf("🎉 Hari ini (*%s*) tidak ada jadwal kuliah.", dayName), nil
	}

	// Check for active class right now
	for _, s := range schedules {
		if s.StartTime != "" && s.EndTime != "" {
			if currentTimeStr >= s.StartTime && currentTimeStr <= s.EndTime {
				var sb strings.Builder
				sb.WriteString(fmt.Sprintf("📍 *Status Kuliah Saat Ini* (Pukul %s %s)\n\n", strings.ReplaceAll(currentTimeStr, ":", "."), dayName))
				sb.WriteString(fmt.Sprintf("Kamu sedang berada di jam mata kuliah:\n📘 *%s*\n🕒 _%s - %s_\n",
					s.Subject, strings.ReplaceAll(s.StartTime, ":", "."), strings.ReplaceAll(s.EndTime, ":", ".")))
				if s.Room != "" {
					sb.WriteString(fmt.Sprintf("📍 %s\n", s.Room))
				}
				if s.Notes != "" {
					sb.WriteString(fmt.Sprintf("📝 _Catatan: %s_\n", s.Notes))
				}
				return sb.String(), nil
			}
		}
	}

	// Check for next upcoming class today
	for _, s := range schedules {
		if s.StartTime != "" && currentTimeStr < s.StartTime {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("🕒 Saat ini (pukul %s) sedang tidak ada kelas aktif.\n\n", strings.ReplaceAll(currentTimeStr, ":", ".")))
			sb.WriteString(fmt.Sprintf("Kelas berikutnya hari ini:\n📘 *%s*\n🕒 _%s - %s_\n",
				s.Subject, strings.ReplaceAll(s.StartTime, ":", "."), strings.ReplaceAll(s.EndTime, ":", ".")))
			if s.Room != "" {
				sb.WriteString(fmt.Sprintf("📍 %s\n", s.Room))
			}
			return sb.String(), nil
		}
	}

	// If all classes have passed
	return fmt.Sprintf("✅ Semua kelas hari ini (*%s*) sudah selesai. Selamat beristirahat!", dayName), nil
}

func (h *Handler) handleSubjectLookup(ctx context.Context, subject string) (string, error) {
	schedules, err := h.db.FindSchedulesBySubjectOrRoom(ctx, subject)
	if err != nil {
		return "", err
	}

	if len(schedules) == 0 {
		return fmt.Sprintf("⚠️ Tidak ditemukan jadwal untuk mata kuliah *%s*.", subject), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 *Informasi Jadwal: %s*\n\n", schedules[0].Subject))

	for i, s := range schedules {
		sb.WriteString(fmt.Sprintf("📅 *%s*\n", s.DayName))
		if s.StartTime != "" && s.EndTime != "" {
			sb.WriteString(fmt.Sprintf("🕒 _%s - %s_\n", strings.ReplaceAll(s.StartTime, ":", "."), strings.ReplaceAll(s.EndTime, ":", ".")))
		}
		if s.Room != "" {
			sb.WriteString(fmt.Sprintf("📍 %s\n", s.Room))
		}
		if s.Notes != "" {
			sb.WriteString(fmt.Sprintf("📝 _Catatan: %s_\n", s.Notes))
		}
		if i < len(schedules)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String(), nil
}

func (h *Handler) handleRoomLookup(ctx context.Context, room string) (string, error) {
	schedules, err := h.db.FindSchedulesBySubjectOrRoom(ctx, room)
	if err != nil {
		return "", err
	}

	if len(schedules) == 0 {
		return fmt.Sprintf("⚠️ Tidak ada kelas yang dijadwalkan di *%s*.", room), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📍 *Penggunaan Ruangan: %s*\n\n", room))

	for i, s := range schedules {
		sb.WriteString(fmt.Sprintf("📅 *%s*", s.DayName))
		if s.StartTime != "" && s.EndTime != "" {
			sb.WriteString(fmt.Sprintf(" (_%s - %s_)", strings.ReplaceAll(s.StartTime, ":", "."), strings.ReplaceAll(s.EndTime, ":", ".")))
		}
		sb.WriteString(fmt.Sprintf("\n📘 *%s*\n", s.Subject))
		if i < len(schedules)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String(), nil
}

// calculateGaps computes gaps >= 60 minutes between consecutive classes in a day.
func calculateGaps(schedules []database.Schedule) []string {
	type timedClass struct {
		start   time.Time
		end     time.Time
		subject string
	}

	var list []timedClass
	const layout = "15:04"

	for _, s := range schedules {
		if s.StartTime == "" || s.EndTime == "" {
			continue
		}
		sClean := strings.ReplaceAll(s.StartTime, ".", ":")
		eClean := strings.ReplaceAll(s.EndTime, ".", ":")

		st, err1 := time.Parse(layout, sClean)
		et, err2 := time.Parse(layout, eClean)
		if err1 == nil && err2 == nil {
			list = append(list, timedClass{start: st, end: et, subject: s.Subject})
		}
	}

	if len(list) < 2 {
		return nil
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].start.Before(list[j].start)
	})

	var gaps []string
	for i := 0; i < len(list)-1; i++ {
		prev := list[i]
		next := list[i+1]

		diffMinutes := int(next.start.Sub(prev.end).Minutes())
		if diffMinutes >= 60 {
			hours := diffMinutes / 60
			mins := diffMinutes % 60

			var durStr string
			if mins > 0 {
				durStr = fmt.Sprintf("%d jam %d menit", hours, mins)
			} else {
				durStr = fmt.Sprintf("%d jam", hours)
			}

			gapMsg := fmt.Sprintf("💡 _Info: Ada jeda kosong %s antara %s dan %s._", durStr, prev.subject, next.subject)
			gaps = append(gaps, gapMsg)
		}
	}

	return gaps
}
