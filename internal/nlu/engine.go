package nlu

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// IntentType represents classified intent.
type IntentType string

const (
	IntentScheduleQuery    IntentType = "schedule.query"
	IntentScheduleOverride IntentType = "schedule.override"
	IntentScheduleSaveRaw  IntentType = "schedule.save_raw"
	IntentKnowledgeQuery   IntentType = "knowledge.query"
	IntentKnowledgeAdd     IntentType = "knowledge.add"
	IntentBotHelp          IntentType = "bot.help"
	IntentAdminCommand     IntentType = "admin.command"
	IntentConversational   IntentType = "conversational"
	IntentUnknown          IntentType = "unknown"
)

// NLUResult holds the parsed understanding of a message.
type NLUResult struct {
	RawInput    string
	Normalized  string
	Corrections []string
	Intent      IntentType
	Confidence  float64
	Entities    *Entities
}

// LLMFallback defines an optional plug-and-play fallback interface for external LLMs.
type LLMFallback interface {
	GenerateResponse(ctx context.Context, prompt string) (string, error)
}

// ChatMessage is one turn in a conversation sent to an LLM.
type ChatMessage struct {
	Role string // "user" or "assistant"
	Text string
}

// ConversationalLLM is implemented by LLM clients that can use conversation history.
type ConversationalLLM interface {
	GenerateConversation(ctx context.Context, history []ChatMessage, prompt string) (string, error)
}

// Engine performs intent classification and entity extraction using deterministic NLU rules.
type Engine struct {
	spellchecker *SpellChecker
	timezone     *time.Location
	llmFallback  LLMFallback
}

// NewEngine creates a new NLU Engine instance.
func NewEngine(sc *SpellChecker, loc *time.Location, llm LLMFallback) *Engine {
	if loc == nil {
		loc = time.Local
	}
	return &Engine{
		spellchecker: sc,
		timezone:     loc,
		llmFallback:  llm,
	}
}

// Process parses a raw WhatsApp message through spellchecking, intent classification, and entity extraction.
func (e *Engine) Process(ctx context.Context, rawText string) (*NLUResult, error) {
	// 1. Remove command prefix (!, /, ., or #) if present
	trimmed := strings.TrimSpace(rawText)
	if strings.HasPrefix(trimmed, "!") || strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, ".") || strings.HasPrefix(trimmed, "#") {
		trimmed = strings.TrimSpace(trimmed[1:])
	}

	// Strip any standalone bot invocation tokens (e.g. "lisa", "ai lisa") anywhere in the message
	botCallRegex := regexp.MustCompile(`(?i)\b(?:ai\s+)?lisa\b`)
	strippedInvocation := strings.TrimSpace(botCallRegex.ReplaceAllString(trimmed, ""))
	if strippedInvocation != "" {
		trimmed = strippedInvocation
	}

	// 2. Perform 3-Layer Spellcheck & Normalization
	normalized, corrections := e.spellchecker.CorrectSentence(trimmed)
	lower := strings.ToLower(normalized)
	rawLower := strings.ToLower(trimmed)

	result := &NLUResult{
		RawInput:    rawText,
		Normalized:  normalized,
		Corrections: corrections,
		Entities:    ExtractEntities(normalized, e.timezone),
	}

	// Check Admin Commands: "izinkan grup", "cabut grup", "daftar grup", "tambah admin", "hapus admin", "daftar admin"
	if strings.HasPrefix(lower, "izinkan grup") || strings.HasPrefix(rawLower, "izinkan grup") ||
		strings.HasPrefix(lower, "cabut grup") || strings.HasPrefix(rawLower, "cabut grup") ||
		strings.HasPrefix(lower, "daftar grup") || strings.HasPrefix(rawLower, "daftar grup") ||
		strings.HasPrefix(lower, "tambah admin") || strings.HasPrefix(rawLower, "tambah admin") ||
		strings.HasPrefix(lower, "hapus admin") || strings.HasPrefix(rawLower, "hapus admin") ||
		strings.HasPrefix(lower, "daftar admin") || strings.HasPrefix(rawLower, "daftar admin") ||
		strings.HasPrefix(lower, "allow group") || strings.HasPrefix(rawLower, "allow group") ||
		strings.HasPrefix(lower, "revoke group") || strings.HasPrefix(rawLower, "revoke group") {
		result.Intent = IntentAdminCommand
		result.Confidence = 1.0
		result.Entities.RawPayload = trimmed
		return result, nil
	}

	// 3. Command Regex Checkers
	// Check: "simpan jadwal"
	if strings.HasPrefix(lower, "simpan jadwal") {
		result.Intent = IntentScheduleSaveRaw
		result.Confidence = 1.0
		idx := strings.Index(strings.ToLower(trimmed), "simpan jadwal")
		payload := strings.TrimSpace(trimmed[idx+len("simpan jadwal"):])
		result.Entities.RawPayload = payload
		return result, nil
	}

	// Check: "info hari ini <matkul> <catatan>"
	overrideRegex := regexp.MustCompile(`(?i)^info\s+hari\s+ini\s+(.+)$`)
	if match := overrideRegex.FindStringSubmatch(trimmed); len(match) > 1 {
		result.Intent = IntentScheduleOverride
		result.Confidence = 1.0
		rest := strings.TrimSpace(match[1])

		// Attempt to extract subject from beginning
		if result.Entities.Subject != "" {
			subLower := strings.ToLower(result.Entities.Subject)
			restLower := strings.ToLower(rest)
			idx := strings.Index(restLower, subLower)
			if idx >= 0 {
				note := strings.TrimSpace(rest[idx+len(subLower):])
				result.Entities.OverrideNote = note
			} else {
				result.Entities.OverrideNote = rest
			}
		} else {
			// First word as subject candidate, rest as note
			parts := strings.SplitN(rest, " ", 2)
			result.Entities.Subject = parts[0]
			if len(parts) > 1 {
				result.Entities.OverrideNote = parts[1]
			}
		}
		return result, nil
	}

	// Check: "tambah info <topik> | <isi info>"
	if strings.HasPrefix(lower, "tambah info") {
		result.Intent = IntentKnowledgeAdd
		result.Confidence = 1.0
		idx := strings.Index(strings.ToLower(trimmed), "tambah info")
		result.Entities.RawPayload = strings.TrimSpace(trimmed[idx+len("tambah info"):])
		return result, nil
	}

	// Check: Bot Help & Utility Menu (support !menu, .ping, /status, etc.)
	if lower == "help" || lower == "menu" || lower == "bantuan" || lower == "info bot" || lower == "ping" || lower == "status" ||
		strings.HasPrefix(lower, "menu ") || strings.HasPrefix(lower, "help ") {
		result.Intent = IntentBotHelp
		result.Confidence = 1.0
		return result, nil
	}

	// 4. Intent Schedule Query vs Knowledge Query vs Conversational
	hasExplicitScheduleWord := containsAny(lower, []string{
		"jadwal", "jadual", "jdwal", "jdwl", "matkul", "mata kuliah", "kelas", "kuliah", "pelajaran",
	}) || containsAny(rawLower, []string{
		"jadwal", "jadual", "jdwal", "jdwl", "matkul", "mata kuliah", "kelas", "kuliah", "pelajaran",
	})

	shortDayInquiry := regexp.MustCompile(`(?i)^(?:(?:ada\s+)?apa\s+(?:besok|bsok|hari ini|hr ini|sekarang|lusa|senin|selasa|rabu|kamis|jumat|sabtu|minggu)|(?:besok|bsok|hari ini|hr ini|sekarang|lusa|senin|selasa|rabu|kamis|jumat|sabtu|minggu)\s+(?:ada\s+)?apa(?:\s+aja|\s+saja)?|(?:besok|bsok|hari ini|hr ini|sekarang|lusa|senin|selasa|rabu|kamis|jumat|sabtu|minggu))\??$`)

	hasDayWithInquiry := (result.Entities.TargetDay > 0 || result.Entities.RelativeTime != RelativeNone) &&
		(hasExplicitScheduleWord || shortDayInquiry.MatchString(lower) || shortDayInquiry.MatchString(rawLower) || containsAny(lower, []string{
			"ada apa", "apa aja", "apa saja", "masuk jam", "jam berapa", "ada kelas", "ada kuliah", "masuk apa", "masuk kapan",
		}))
	hasSubjectInquiry := result.Entities.Subject != "" &&
		(hasExplicitScheduleWord || containsAny(lower, []string{
			"kapan", "hari apa", "jam berapa", "ruang apa", "dimana", "di mana",
		}))

	if hasExplicitScheduleWord || hasDayWithInquiry || hasSubjectInquiry {
		result.Intent = IntentScheduleQuery
		result.Confidence = 0.90
		if result.Entities.TargetDay == 0 && result.Entities.RelativeTime == RelativeNone && result.Entities.Subject == "" && e.llmFallback != nil {
			e.refineScheduleWithLLM(ctx, result.Normalized, result)
		}
		return result, nil
	}

	// 5. Explicit Knowledge Base Signals (Rules, Prohibitions, Lab Regulations)
	hasKnowledgeSignals := containsAny(lower, []string{"larangan", "aturan", "praktikum", "tata tertib", "lab", "laboratorium", "syarat", "ujian", "uts", "uas", "beasiswa", "biaya"})
	if hasKnowledgeSignals {
		result.Intent = IntentKnowledgeQuery
		result.Confidence = 0.85
		result.Entities.KnowledgeTopic = extractKnowledgeTopic(lower)
		return result, nil
	}

	// 6. Conversational / General Inquiries (Handled by Gemini AI or Lisa Persona)
	result.Intent = IntentConversational
	result.Confidence = 0.80
	return result, nil
}

type llmExtraction struct {
	Intent    string `json:"intent"`
	DayOffset *int   `json:"day_offset"`
	DayName   string `json:"day_name"`
	Subject   string `json:"subject"`
}

func parseLLMExtraction(raw string) (*llmExtraction, bool) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, false
	}
	var ext llmExtraction
	if err := json.Unmarshal([]byte(raw[start:end+1]), &ext); err != nil {
		return nil, false
	}
	return &ext, true
}

// refineScheduleWithLLM asks the LLM to extract date/subject from schedule queries
// that the deterministic rules could not map to a specific day.
func (e *Engine) refineScheduleWithLLM(ctx context.Context, text string, result *NLUResult) {
	if e.llmFallback == nil || result == nil || result.Entities == nil {
		return
	}
	ext, ok := e.askLLMExtraction(ctx, text)
	if !ok {
		return
	}
	if ext.Intent != "" && ext.Intent != string(IntentScheduleQuery) {
		return
	}

	if ext.DayOffset != nil && *ext.DayOffset >= 0 && *ext.DayOffset <= 60 {
		applyTargetDate(result.Entities, time.Now().In(e.timezone).AddDate(0, 0, *ext.DayOffset))
		return
	}

	if ext.DayName != "" {
		if day, found := DayNameMap[strings.ToLower(strings.TrimSpace(ext.DayName))]; found {
			result.Entities.TargetDay = day
			result.Entities.TargetDayName = IntToDayName[day]
			return
		}
	}

	if ext.Subject != "" && result.Entities.Subject == "" {
		result.Entities.Subject = strings.TrimSpace(ext.Subject)
	}
}

func (e *Engine) askLLMExtraction(ctx context.Context, text string) (*llmExtraction, bool) {
	if e.llmFallback == nil || strings.TrimSpace(text) == "" {
		return nil, false
	}

	prompt := "Kamu adalah parser intent untuk bot jadwal kuliah berbahasa Indonesia. " +
		"Balas HANYA JSON valid tanpa penjelasan dan tanpa markdown, dengan format persis:\n" +
		`{"intent":"schedule.query|knowledge.query|conversational","day_offset":null,"day_name":null,"subject":null}` + "\n" +
		"Aturan: intent schedule.query hanya kalau pengguna menanyakan jadwal/kelas/mata kuliah; " +
		"day_offset = jumlah hari dari hari ini (angka bulat, contoh \"3 hari lagi\" = 3, \"minggu depan\" = 7); " +
		"day_name = nama hari Indonesia (senin..minggu) kalau disebut; subject = nama mata kuliah kalau disebut. " +
		"Kalau tidak ada, isi null.\n" +
		fmt.Sprintf("Pesan pengguna: %q", text)

	resp, err := e.llmFallback.GenerateResponse(ctx, prompt)
	if err != nil || strings.TrimSpace(resp) == "" {
		return nil, false
	}
	return parseLLMExtraction(resp)
}

// extractKnowledgeTopic extracts the primary keyword topic for knowledge lookup.
func extractKnowledgeTopic(s string) string {
	topics := []string{"larangan", "aturan", "praktikum", "tata tertib", "lab", "info bot", "bantuan"}
	for _, t := range topics {
		if strings.Contains(s, t) {
			return t
		}
	}
	return s
}
