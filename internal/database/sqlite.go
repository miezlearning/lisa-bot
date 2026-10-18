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
	query := `SELECT id, topic, keywords, content, updated_at 
	          FROM knowledge_base 
	          WHERE LOWER(topic) LIKE ? OR LOWER(keywords) LIKE ? OR LOWER(content) LIKE ?`

	pattern := "%" + cleaned + "%"
	rows, err := db.Conn.QueryContext(ctx, query, pattern, pattern, pattern)
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
