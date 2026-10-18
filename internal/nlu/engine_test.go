package nlu

import (
	"context"
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
