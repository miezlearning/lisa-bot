package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Schedule represents a lecture schedule row in the database.
type Schedule struct {
	ID        int64  `json:"id"`
	DayOfWeek int    `json:"day_of_week"` // 1 = Monday, ..., 7 = Sunday
	DayName   string `json:"day_name"`
	StartTime string `json:"start_time"` // Format "HH:MM", nullable/empty
	EndTime   string `json:"end_time"`   // Format "HH:MM", nullable/empty
	Subject   string `json:"subject"`
	Room      string `json:"room"`  // e.g. "Ruang B.204"
	Notes     string `json:"notes"` // e.g. "pisah" or notes
}

// ScheduleOverride represents crowd-sourced real-time updates for classes on a specific date.
type ScheduleOverride struct {
	ID           int64     `json:"id"`
	OverrideDate string    `json:"override_date"` // Format "YYYY-MM-DD"
	Subject      string    `json:"subject"`
	Note         string    `json:"note"`
	SenderJID    string    `json:"sender_jid"`
	SenderName   string    `json:"sender_name"`
	CreatedAt    time.Time `json:"created_at"`
}

// KnowledgeItem represents a record in knowledge_base table.
type KnowledgeItem struct {
	ID        int64     `json:"id"`
	Topic     string    `json:"topic"`
	Keywords  string    `json:"keywords"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AllowedGroup represents a group chat authorized to interact with the bot.
type AllowedGroup struct {
	GroupJID  string    `json:"group_jid"`
	GroupName string    `json:"group_name"`
	AddedBy   string    `json:"added_by"`
	CreatedAt time.Time `json:"created_at"`
}

// BotAdmin represents an authorized phone number that can manage bot settings.
type BotAdmin struct {
	PhoneNumber string    `json:"phone_number"` // Normalised digits e.g. "62812345678"
	Name        string    `json:"name"`
	AddedBy     string    `json:"added_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// Reminder represents a scheduled reminder or deadline row.
type Reminder struct {
	ID         int64     `json:"id"`
	ChatJID    string    `json:"chat_jid"`
	SenderJID  string    `json:"sender_jid"`
	SenderName string    `json:"sender_name"`
	Kind       string    `json:"kind"` // "reminder" or "deadline"
	Message    string    `json:"message"`
	DueAt      time.Time `json:"due_at"`
	Fired      bool      `json:"fired"`
	CreatedAt  time.Time `json:"created_at"`
}

// Briefing represents morning briefing settings for a chat.
type Briefing struct {
	ChatJID      string    `json:"chat_jid"`
	TimeHM       string    `json:"time_hm"` // Format "HH:MM"
	Enabled      bool      `json:"enabled"`
	LastSentDate string    `json:"last_sent_date"` // Format "YYYY-MM-DD"
	UpdatedAt    time.Time `json:"updated_at"`
}

// DB wraps the sql.DB instance and provides access methods.
type DB struct {
	Conn *sql.DB
}

// NewDB initializes connection to SQLite database and runs auto-migration.
func NewDB(dbPath string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", dbPath)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	conn.SetMaxOpenConns(1) // SQLite single-writer safe mode

	db := &DB{Conn: conn}
	if err := db.autoMigrate(context.Background()); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("auto migration failed: %w", err)
	}

	return db, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.Conn.Close()
}

func (db *DB) autoMigrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS schedules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			day_of_week INTEGER NOT NULL,
			day_name TEXT NOT NULL,
			start_time TEXT,
			end_time TEXT,
			subject TEXT NOT NULL,
			room TEXT,
			notes TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_schedules_day ON schedules(day_of_week);`,
		`CREATE TABLE IF NOT EXISTS schedule_overrides (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			override_date TEXT NOT NULL,
			subject TEXT NOT NULL,
			note TEXT NOT NULL,
			sender_jid TEXT,
			sender_name TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_overrides_date ON schedule_overrides(override_date);`,
		`CREATE TABLE IF NOT EXISTS knowledge_base (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			topic TEXT NOT NULL UNIQUE,
			keywords TEXT NOT NULL,
			content TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_topic ON knowledge_base(topic);`,
		`CREATE TABLE IF NOT EXISTS allowed_groups (
			group_jid TEXT PRIMARY KEY,
			group_name TEXT,
			added_by TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS bot_admins (
			phone_number TEXT PRIMARY KEY,
			name TEXT,
			added_by TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS reminders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_jid TEXT NOT NULL,
			sender_jid TEXT,
			sender_name TEXT,
			kind TEXT NOT NULL DEFAULT 'reminder',
			message TEXT NOT NULL,
			due_at INTEGER NOT NULL,
			fired INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_reminders_pending ON reminders(fired, due_at);`,
		`CREATE INDEX IF NOT EXISTS idx_reminders_chat ON reminders(chat_jid, fired, due_at);`,
		`CREATE TABLE IF NOT EXISTS briefings (
			chat_jid TEXT PRIMARY KEY,
			time_hm TEXT NOT NULL DEFAULT '06:30',
			enabled INTEGER NOT NULL DEFAULT 1,
			last_sent_date TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, query := range queries {
		if _, err := db.Conn.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to execute migration query (%s): %w", query, err)
		}
	}

	return nil
}

// GetSchedulesByDay retrieves all schedules for a specific day of week (1=Senin, ..., 7=Minggu).
func (db *DB) GetSchedulesByDay(ctx context.Context, dayOfWeek int) ([]Schedule, error) {
	query := `SELECT id, day_of_week, day_name, COALESCE(start_time, ''), COALESCE(end_time, ''), subject, COALESCE(room, ''), COALESCE(notes, '') 
	          FROM schedules 
	          WHERE day_of_week = ? 
	          ORDER BY CASE WHEN start_time = '' OR start_time IS NULL THEN '99:99' ELSE start_time END ASC, id ASC`

	rows, err := db.Conn.QueryContext(ctx, query, dayOfWeek)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Schedule
	for rows.Next() {
		var s Schedule
		if err := rows.Scan(&s.ID, &s.DayOfWeek, &s.DayName, &s.StartTime, &s.EndTime, &s.Subject, &s.Room, &s.Notes); err != nil {
			return nil, err
		}
		list = append(list, s)
	}

	return list, rows.Err()
}

// GetAllSchedules retrieves all schedules stored in the database.
func (db *DB) GetAllSchedules(ctx context.Context) ([]Schedule, error) {
	query := `SELECT id, day_of_week, day_name, COALESCE(start_time, ''), COALESCE(end_time, ''), subject, COALESCE(room, ''), COALESCE(notes, '') 
	          FROM schedules 
	          ORDER BY day_of_week ASC, CASE WHEN start_time = '' OR start_time IS NULL THEN '99:99' ELSE start_time END ASC`

	rows, err := db.Conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Schedule
	for rows.Next() {
		var s Schedule
		if err := rows.Scan(&s.ID, &s.DayOfWeek, &s.DayName, &s.StartTime, &s.EndTime, &s.Subject, &s.Room, &s.Notes); err != nil {
			return nil, err
		}
		list = append(list, s)
	}

	return list, rows.Err()
}

// FindSchedulesBySubjectOrRoom performs a case-insensitive search for subject or room.
func (db *DB) FindSchedulesBySubjectOrRoom(ctx context.Context, term string) ([]Schedule, error) {
	pattern := "%" + strings.TrimSpace(term) + "%"
	query := `SELECT id, day_of_week, day_name, COALESCE(start_time, ''), COALESCE(end_time, ''), subject, COALESCE(room, ''), COALESCE(notes, '') 
	          FROM schedules 
	          WHERE LOWER(subject) LIKE LOWER(?) OR LOWER(room) LIKE LOWER(?)
	          ORDER BY day_of_week ASC, CASE WHEN start_time = '' OR start_time IS NULL THEN '99:99' ELSE start_time END ASC`

	rows, err := db.Conn.QueryContext(ctx, query, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Schedule
	for rows.Next() {
		var s Schedule
		if err := rows.Scan(&s.ID, &s.DayOfWeek, &s.DayName, &s.StartTime, &s.EndTime, &s.Subject, &s.Room, &s.Notes); err != nil {
			return nil, err
		}
		list = append(list, s)
	}

	return list, rows.Err()
}

// ClearAndInsertSchedules replaces all current schedules with newly parsed schedules.
func (db *DB) ClearAndInsertSchedules(ctx context.Context, schedules []Schedule) error {
	tx, err := db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM schedules`); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO schedules (day_of_week, day_name, start_time, end_time, subject, room, notes) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range schedules {
		if _, err := stmt.ExecContext(ctx, s.DayOfWeek, s.DayName, s.StartTime, s.EndTime, s.Subject, s.Room, s.Notes); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// AddScheduleOverride inserts a daily class status override.
func (db *DB) AddScheduleOverride(ctx context.Context, override ScheduleOverride) error {
	query := `INSERT INTO schedule_overrides (override_date, subject, note, sender_jid, sender_name) VALUES (?, ?, ?, ?, ?)`
	_, err := db.Conn.ExecContext(ctx, query, override.OverrideDate, override.Subject, override.Note, override.SenderJID, override.SenderName)
	return err
}

// GetOverridesByDate returns all overrides recorded for a specific date (YYYY-MM-DD).
func (db *DB) GetOverridesByDate(ctx context.Context, dateStr string) ([]ScheduleOverride, error) {
	query := `SELECT id, override_date, subject, note, COALESCE(sender_jid, ''), COALESCE(sender_name, ''), created_at 
	          FROM schedule_overrides 
	          WHERE override_date = ? 
	          ORDER BY id ASC`

	rows, err := db.Conn.QueryContext(ctx, query, dateStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ScheduleOverride
	for rows.Next() {
		var o ScheduleOverride
		if err := rows.Scan(&o.ID, &o.OverrideDate, &o.Subject, &o.Note, &o.SenderJID, &o.SenderName, &o.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, o)
	}

	return list, rows.Err()
}

// GetAllKnowledge retrieves all knowledge base entries.
func (db *DB) GetAllKnowledge(ctx context.Context) ([]KnowledgeItem, error) {
	query := `SELECT id, topic, keywords, content, updated_at FROM knowledge_base ORDER BY id ASC`
	rows, err := db.Conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []KnowledgeItem
	for rows.Next() {
		var k KnowledgeItem
		if err := rows.Scan(&k.ID, &k.Topic, &k.Keywords, &k.Content, &k.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, k)
	}

	return list, rows.Err()
}

// FindKnowledge searches for knowledge items matching topic or keywords.
func (db *DB) FindKnowledge(ctx context.Context, term string) ([]KnowledgeItem, error) {
	cleaned := strings.ToLower(strings.TrimSpace(term))
	if cleaned == "" {
		return nil, nil
	}
	query := `SELECT id, topic, keywords, content, updated_at 
	          FROM knowledge_base 
	          WHERE LOWER(topic) = ? OR LOWER(topic) LIKE ? OR LOWER(keywords) LIKE ?
	          ORDER BY CASE WHEN LOWER(topic) = ? THEN 1 WHEN LOWER(topic) LIKE ? THEN 2 ELSE 3 END ASC`

	pattern := "%" + cleaned + "%"
	rows, err := db.Conn.QueryContext(ctx, query, cleaned, pattern, pattern, cleaned, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []KnowledgeItem
	for rows.Next() {
		var k KnowledgeItem
		if err := rows.Scan(&k.ID, &k.Topic, &k.Keywords, &k.Content, &k.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, k)
	}

	return list, rows.Err()
}

// UpsertKnowledge inserts or updates a topic in knowledge_base.
func (db *DB) UpsertKnowledge(ctx context.Context, topic, keywords, content string) error {
	query := `INSERT INTO knowledge_base (topic, keywords, content, updated_at) 
	          VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	          ON CONFLICT(topic) DO UPDATE SET 
	             keywords = excluded.keywords, 
	             content = excluded.content, 
	             updated_at = CURRENT_TIMESTAMP`
	_, err := db.Conn.ExecContext(ctx, query, strings.ToLower(strings.TrimSpace(topic)), keywords, content)
	return err
}

// NormalizePhoneNumber cleans a phone number to digits only (e.g. +62 812-3456 -> 628123456).
func NormalizePhoneNumber(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if strings.HasPrefix(s, "08") {
		s = "628" + s[2:]
	}
	return s
}

// IsGroupAllowed checks if a group JID is authorized to interact with the bot.
func (db *DB) IsGroupAllowed(ctx context.Context, groupJID string) bool {
	var dummy int
	err := db.Conn.QueryRowContext(ctx, `SELECT 1 FROM allowed_groups WHERE group_jid = ?`, groupJID).Scan(&dummy)
	return err == nil
}

// AllowGroup adds or updates an authorized group.
func (db *DB) AllowGroup(ctx context.Context, groupJID, groupName, addedBy string) error {
	query := `INSERT INTO allowed_groups (group_jid, group_name, added_by, created_at) 
	          VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	          ON CONFLICT(group_jid) DO UPDATE SET group_name = excluded.group_name`
	_, err := db.Conn.ExecContext(ctx, query, groupJID, groupName, addedBy)
	return err
}

// RevokeGroup removes a group from the whitelist.
func (db *DB) RevokeGroup(ctx context.Context, groupJID string) error {
	query := `DELETE FROM allowed_groups WHERE group_jid = ?`
	_, err := db.Conn.ExecContext(ctx, query, groupJID)
	return err
}

// GetAllowedGroups returns all whitelisted groups.
func (db *DB) GetAllowedGroups(ctx context.Context) ([]AllowedGroup, error) {
	query := `SELECT group_jid, group_name, added_by, created_at FROM allowed_groups ORDER BY created_at DESC`
	rows, err := db.Conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []AllowedGroup
	for rows.Next() {
		var g AllowedGroup
		if err := rows.Scan(&g.GroupJID, &g.GroupName, &g.AddedBy, &g.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, g)
	}
	return list, rows.Err()
}

// IsAdmin checks if the given phone number belongs to a registered admin.
func (db *DB) IsAdmin(ctx context.Context, phone string) bool {
	normalized := NormalizePhoneNumber(phone)
	if normalized == "" {
		return false
	}
	var dummy int
	err := db.Conn.QueryRowContext(ctx, `SELECT 1 FROM bot_admins WHERE phone_number = ?`, normalized).Scan(&dummy)
	return err == nil
}

// AddAdmin registers a new admin phone number.
func (db *DB) AddAdmin(ctx context.Context, phone, name, addedBy string) error {
	normalized := NormalizePhoneNumber(phone)
	if normalized == "" {
		return fmt.Errorf("nomor telepon admin tidak valid")
	}
	query := `INSERT INTO bot_admins (phone_number, name, added_by, created_at) 
	          VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	          ON CONFLICT(phone_number) DO UPDATE SET name = excluded.name`
	_, err := db.Conn.ExecContext(ctx, query, normalized, name, addedBy)
	return err
}

// RemoveAdmin deletes an admin phone number.
func (db *DB) RemoveAdmin(ctx context.Context, phone string) error {
	normalized := NormalizePhoneNumber(phone)
	query := `DELETE FROM bot_admins WHERE phone_number = ?`
	_, err := db.Conn.ExecContext(ctx, query, normalized)
	return err
}

// GetAdmins returns all registered admin records.
func (db *DB) GetAdmins(ctx context.Context) ([]BotAdmin, error) {
	query := `SELECT phone_number, name, added_by, created_at FROM bot_admins ORDER BY created_at ASC`
	rows, err := db.Conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []BotAdmin
	for rows.Next() {
		var a BotAdmin
		if err := rows.Scan(&a.PhoneNumber, &a.Name, &a.AddedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// GetAdminCount returns the total number of registered administrators.
func (db *DB) GetAdminCount(ctx context.Context) (int, error) {
	var count int
	err := db.Conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_admins`).Scan(&count)
	return count, err
}

const reminderSelectColumns = `id, chat_jid, COALESCE(sender_jid, ''), COALESCE(sender_name, ''), kind, message, due_at, fired, created_at`

func scanReminder(scanner interface{ Scan(...any) error }) (Reminder, error) {
	var r Reminder
	var dueUnix int64
	var fired int
	if err := scanner.Scan(&r.ID, &r.ChatJID, &r.SenderJID, &r.SenderName, &r.Kind, &r.Message, &dueUnix, &fired, &r.CreatedAt); err != nil {
		return Reminder{}, err
	}
	r.DueAt = time.Unix(dueUnix, 0)
	r.Fired = fired != 0
	return r, nil
}

// AddReminder inserts a new reminder and returns its ID.
func (db *DB) AddReminder(ctx context.Context, reminder Reminder) (int64, error) {
	query := `INSERT INTO reminders (chat_jid, sender_jid, sender_name, kind, message, due_at) VALUES (?, ?, ?, ?, ?, ?)`
	res, err := db.Conn.ExecContext(ctx, query, reminder.ChatJID, reminder.SenderJID, reminder.SenderName, reminder.Kind, reminder.Message, reminder.DueAt.Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetReminder returns a reminder by ID, or nil when it does not exist.
func (db *DB) GetReminder(ctx context.Context, id int64) (*Reminder, error) {
	query := `SELECT ` + reminderSelectColumns + ` FROM reminders WHERE id = ?`
	reminder, err := scanReminder(db.Conn.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &reminder, nil
}

// GetActiveReminders returns unfired reminders for a chat ordered by due time.
func (db *DB) GetActiveReminders(ctx context.Context, chatJID string, limit int) ([]Reminder, error) {
	if limit <= 0 {
		limit = 20
	}
	query := `SELECT ` + reminderSelectColumns + ` FROM reminders WHERE chat_jid = ? AND fired = 0 ORDER BY due_at ASC LIMIT ?`
	rows, err := db.Conn.QueryContext(ctx, query, chatJID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Reminder
	for rows.Next() {
		reminder, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, reminder)
	}
	return list, rows.Err()
}

// GetDueReminders returns unfired reminders whose due time has passed.
func (db *DB) GetDueReminders(ctx context.Context, now time.Time, limit int) ([]Reminder, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT ` + reminderSelectColumns + ` FROM reminders WHERE fired = 0 AND due_at <= ? ORDER BY due_at ASC LIMIT ?`
	rows, err := db.Conn.QueryContext(ctx, query, now.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Reminder
	for rows.Next() {
		reminder, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, reminder)
	}
	return list, rows.Err()
}

// GetUpcomingDeadlines returns unfired deadlines within the given time range.
func (db *DB) GetUpcomingDeadlines(ctx context.Context, chatJID string, from, until time.Time) ([]Reminder, error) {
	query := `SELECT ` + reminderSelectColumns + ` FROM reminders
	          WHERE chat_jid = ? AND kind = 'deadline' AND fired = 0 AND due_at >= ? AND due_at <= ?
	          ORDER BY due_at ASC LIMIT 20`
	rows, err := db.Conn.QueryContext(ctx, query, chatJID, from.Unix(), until.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Reminder
	for rows.Next() {
		reminder, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, reminder)
	}
	return list, rows.Err()
}

// MarkReminderFired marks a reminder as already sent.
func (db *DB) MarkReminderFired(ctx context.Context, id int64) error {
	_, err := db.Conn.ExecContext(ctx, `UPDATE reminders SET fired = 1 WHERE id = ?`, id)
	return err
}

// DeleteReminder removes a reminder by ID.
func (db *DB) DeleteReminder(ctx context.Context, id int64) error {
	_, err := db.Conn.ExecContext(ctx, `DELETE FROM reminders WHERE id = ?`, id)
	return err
}

// GetBriefing returns briefing settings for a chat, or nil when none exist.
func (db *DB) GetBriefing(ctx context.Context, chatJID string) (*Briefing, error) {
	query := `SELECT chat_jid, time_hm, enabled, COALESCE(last_sent_date, ''), updated_at FROM briefings WHERE chat_jid = ?`
	var b Briefing
	var enabled int
	err := db.Conn.QueryRowContext(ctx, query, chatJID).Scan(&b.ChatJID, &b.TimeHM, &enabled, &b.LastSentDate, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.Enabled = enabled != 0
	return &b, nil
}

// UpsertBriefing creates or updates briefing settings for a chat.
func (db *DB) UpsertBriefing(ctx context.Context, chatJID, timeHM string, enabled bool) error {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	query := `INSERT INTO briefings (chat_jid, time_hm, enabled, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	          ON CONFLICT(chat_jid) DO UPDATE SET time_hm = excluded.time_hm, enabled = excluded.enabled, updated_at = CURRENT_TIMESTAMP`
	_, err := db.Conn.ExecContext(ctx, query, chatJID, timeHM, enabledInt)
	return err
}

// GetEnabledBriefings returns all chats with an active morning briefing.
func (db *DB) GetEnabledBriefings(ctx context.Context) ([]Briefing, error) {
	query := `SELECT chat_jid, time_hm, enabled, COALESCE(last_sent_date, ''), updated_at FROM briefings WHERE enabled = 1 ORDER BY chat_jid ASC`
	rows, err := db.Conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Briefing
	for rows.Next() {
		var b Briefing
		var enabled int
		if err := rows.Scan(&b.ChatJID, &b.TimeHM, &enabled, &b.LastSentDate, &b.UpdatedAt); err != nil {
			return nil, err
		}
		b.Enabled = enabled != 0
		list = append(list, b)
	}
	return list, rows.Err()
}

// MarkBriefingSent records the date a briefing was last delivered.
func (db *DB) MarkBriefingSent(ctx context.Context, chatJID, date string) error {
	_, err := db.Conn.ExecContext(ctx, `UPDATE briefings SET last_sent_date = ? WHERE chat_jid = ?`, date, chatJID)
	return err
}
