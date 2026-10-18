package nlu

import (
	"context"
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
	// 1. Remove command prefix (! or /) if present
	trimmed := strings.TrimSpace(rawText)
	if strings.HasPrefix(trimmed, "!") || strings.HasPrefix(trimmed, "/") {
		trimmed = strings.TrimSpace(trimmed[1:])
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

	// Check: Bot Help Menu
	if lower == "help" || lower == "menu" || lower == "bantuan" || lower == "info bot" {
		result.Intent = IntentBotHelp
		result.Confidence = 1.0
		return result, nil
	}

	// 4. Intent Schedule Query vs Knowledge Query vs Conversational
	hasExplicitScheduleWord := containsAny(lower, []string{"jadwal", "matkul", "mata kuliah", "kelas", "kuliah", "pelajaran"})
	hasDayWithInquiry := (result.Entities.TargetDay > 0 || result.Entities.RelativeTime != RelativeNone) &&
		(hasExplicitScheduleWord || containsAny(lower, []string{"ada apa", "apa aja", "apa saja", "masuk jam", "jam berapa", "ada kelas", "ada kuliah"}))
	hasSubjectInquiry := result.Entities.Subject != "" &&
		(hasExplicitScheduleWord || containsAny(lower, []string{"kapan", "hari apa", "jam berapa", "ruang apa", "dimana", "di mana"}))

	if hasExplicitScheduleWord || hasDayWithInquiry || hasSubjectInquiry {
		result.Intent = IntentScheduleQuery
		result.Confidence = 0.90
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
