package reminder

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"whatsapp-bot/internal/database"
)

const (
	schedulerInterval = 30 * time.Second
	briefingWindow    = 3 * time.Hour
)

// Scheduler periodically fires due reminders and morning briefings.
type Scheduler struct {
	db       *database.DB
	timezone *time.Location
	build    func(context.Context, string, time.Time) string
	send     func(chatJID, text string)
	now      func() time.Time
	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewScheduler creates a scheduler that uses build for briefings and send for delivery.
func NewScheduler(db *database.DB, loc *time.Location, build func(context.Context, string, time.Time) string, send func(chatJID, text string)) *Scheduler {
	if loc == nil {
		loc = time.Local
	}
	if build == nil {
		build = func(context.Context, string, time.Time) string { return "" }
	}
	if send == nil {
		send = func(string, string) {}
	}
	return &Scheduler{
		db:       db,
		timezone: loc,
		build:    build,
		send:     send,
		now:      time.Now,
		stop:     make(chan struct{}),
	}
}

// Start runs the scheduler until Stop is called.
func (s *Scheduler) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		ticker := time.NewTicker(schedulerInterval)
		defer ticker.Stop()

		s.tick()

		for {
			select {
			case <-ticker.C:
				s.tick()
			case <-s.stop:
				return
			}
		}
	}()
}

// Stop shuts the scheduler down and waits for the current tick to finish.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stop)
	})
	s.wg.Wait()
}

func (s *Scheduler) tick() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	now := s.now().In(s.timezone)
	s.fireReminders(ctx, now)
	s.sendBriefings(ctx, now)
}

func (s *Scheduler) fireReminders(ctx context.Context, now time.Time) {
	due, err := s.db.GetDueReminders(ctx, now, 50)
	if err != nil {
		fmt.Printf("⚠️ [Reminder] Gagal mengambil pengingat: %v\n", err)
		return
	}

	for _, item := range due {
		s.send(item.ChatJID, formatReminderNotification(item, s.timezone))
		if err := s.db.MarkReminderFired(ctx, item.ID); err != nil {
			fmt.Printf("⚠️ [Reminder] Gagal menandai pengingat #%d: %v\n", item.ID, err)
			continue
		}
		fmt.Printf("⏰ [Reminder] Pengingat #%d terkirim ke %s\n", item.ID, item.ChatJID)
	}
}

func (s *Scheduler) sendBriefings(ctx context.Context, now time.Time) {
	list, err := s.db.GetEnabledBriefings(ctx)
	if err != nil {
		fmt.Printf("⚠️ [Briefing] Gagal mengambil pengaturan briefing: %v\n", err)
		return
	}

	today := now.Format("2006-01-02")
	for _, item := range list {
		if item.LastSentDate == today {
			continue
		}

		hour, minute, ok := parseTimeHM(item.TimeHM)
		if !ok {
			continue
		}

		scheduled := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, s.timezone)
		if now.Before(scheduled) {
			continue
		}
		if now.After(scheduled.Add(briefingWindow)) {
			continue
		}

		text := s.build(ctx, item.ChatJID, now)
		if strings.TrimSpace(text) == "" {
			continue
		}

		s.send(item.ChatJID, text)
		if err := s.db.MarkBriefingSent(ctx, item.ChatJID, today); err != nil {
			fmt.Printf("⚠️ [Briefing] Gagal menandai briefing %s: %v\n", item.ChatJID, err)
			continue
		}
		fmt.Printf("🌅 [Briefing] Briefing terkirim ke %s\n", item.ChatJID)
	}
}

func formatReminderNotification(item database.Reminder, loc *time.Location) string {
	due := item.DueAt.In(loc).Format("02 Jan 2006 • 15:04")
	if item.Kind == string(KindDeadline) {
		return fmt.Sprintf("📌 *DEADLINE TIBA!*\n\n📝 %s\n🕒 %s\n\n_Semoga tugasnya sudah selesai ya!_", item.Message, due)
	}

	text := fmt.Sprintf("⏰ *PENGINGAT!*\n\n📝 %s\n🕒 %s", item.Message, due)
	if name := strings.TrimSpace(item.SenderName); name != "" {
		text += fmt.Sprintf("\n\n_Dibuat oleh %s._", name)
	}
	return text
}
