package nlu

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestEngineClassification(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Makassar")
	sc := NewSpellChecker()
	sc.LearnFromDatabaseTexts([]string{"Pengantar Ilmu Hukum", "Ilmu Negara", "Pend. Pancasila"})

	engine := NewEngine(sc, loc, nil)
	ctx := context.Background()

	tests := []struct {
		input          string
		expectedIntent IntentType
	}{
		{
			input:          "lisa, jdwl bsk apa",
			expectedIntent: IntentScheduleQuery,
		},
		{
			input:          "kalau aku besok ga turun dosen marah ga?",
			expectedIntent: IntentConversational,
		},
		{
			input:          "siapa namamu dan apa tugasmu?",
			expectedIntent: IntentConversational,
		},
		{
			input:          "apa larangan praktikum?",
			expectedIntent: IntentKnowledgeQuery,
		},
		{
			input:          "kapan kelas pih?",
			expectedIntent: IntentScheduleQuery,
		},
		{
			input:          "apa jdwalku besok @AI Lisa",
			expectedIntent: IntentScheduleQuery,
		},
		{
			input:          "apa jadwalku",
			expectedIntent: IntentScheduleQuery,
		},
		{
			input:          "jadwalku besok",
			expectedIntent: IntentScheduleQuery,
		},
		{
			input:          "besok apa",
			expectedIntent: IntentScheduleQuery,
		},
		{
			input:          "besok ada apa",
			expectedIntent: IntentScheduleQuery,
		},
		{
			input:          "besok",
			expectedIntent: IntentScheduleQuery,
		},
		{
			input:          "bsk jdwlny ap y",
			expectedIntent: IntentScheduleQuery,
		},
		{
			input:          "lisa",
			expectedIntent: IntentConversational,
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			res, err := engine.Process(ctx, tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Intent != tc.expectedIntent {
				t.Errorf("input %q: expected intent %s, got %s", tc.input, tc.expectedIntent, res.Intent)
			}
		})
	}
}

func TestPersonaInstruction(t *testing.T) {
	t.Setenv("BOT_PERSONA", "")
	t.Setenv("BOT_PERSONA_FILE", "")

	def := buildSystemInstruction(loadPersona())
	if !strings.Contains(def, "GAYA BICARA") || !strings.Contains(def, `"mi"`) {
		t.Errorf("default persona missing style block: %s", def)
	}

	custom := "Balasan pendek, suka bilang wkwk"
	t.Setenv("BOT_PERSONA", custom)
	got := buildSystemInstruction(loadPersona())
	if !strings.Contains(got, custom) {
		t.Errorf("custom persona from env not applied: %s", got)
	}
	if !strings.Contains(got, "JANGAN PERNAH menyebut bahwa kamu adalah Gemini") {
		t.Errorf("base instruction lost when custom persona applied")
	}
}
