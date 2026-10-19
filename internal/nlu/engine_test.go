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
	if !strings.Contains(def, "BUGIS") || !strings.Contains(def, `"mandre"`) || !strings.Contains(def, "tegaki monro") {
		t.Errorf("bugis language block missing from system instruction")
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
	if !strings.Contains(got, "BUGIS") {
		t.Errorf("bugis language block lost when custom persona applied")
	}
}

type fakeLLM struct {
	reply string
}

func (f *fakeLLM) GenerateResponse(ctx context.Context, prompt string) (string, error) {
	return f.reply, nil
}

func TestRelativeDayOffsets(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Makassar")
	sc := NewSpellChecker()
	sc.LearnFromDatabaseTexts([]string{"Pengantar Ilmu Hukum"})
	engine := NewEngine(sc, loc, nil)
	ctx := context.Background()
	now := time.Now().In(loc)

	cases := []struct {
		input  string
		offset int
	}{
		{input: "jadwal 3 hari lagi apa", offset: 3},
		{input: "jadwal 2 hari lgi apa", offset: 2},
		{input: "jadwal minggu depan", offset: 7},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			res, err := engine.Process(ctx, tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			target := now.AddDate(0, 0, tc.offset)
			wantDay := GoWeekdayToScheduleDay(target.Weekday())
			if res.Intent != IntentScheduleQuery {
				t.Errorf("input %q: expected intent %s, got %s", tc.input, IntentScheduleQuery, res.Intent)
			}
			if res.Entities.TargetDay != wantDay {
				t.Errorf("input %q: expected targetDay %d, got %d", tc.input, wantDay, res.Entities.TargetDay)
			}
			if res.Entities.TargetDate != target.Format("2006-01-02") {
				t.Errorf("input %q: expected targetDate %s, got %s", tc.input, target.Format("2006-01-02"), res.Entities.TargetDate)
			}
		})
	}
}

func TestLLMRefineSchedule(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Makassar")
	sc := NewSpellChecker()
	fake := &fakeLLM{reply: `{"intent":"schedule.query","day_offset":2,"day_name":null,"subject":null}`}
	engine := NewEngine(sc, loc, fake)
	ctx := context.Background()
	now := time.Now().In(loc)

	res, err := engine.Process(ctx, "jadwal tanggal berapa ya")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	target := now.AddDate(0, 0, 2)
	if res.Entities.TargetDay != GoWeekdayToScheduleDay(target.Weekday()) || res.Entities.TargetDate != target.Format("2006-01-02") {
		t.Errorf("expected LLM offset to set target day %s (%s), got day %d (%s)",
			IntToDayName[GoWeekdayToScheduleDay(target.Weekday())], target.Format("2006-01-02"), res.Entities.TargetDay, res.Entities.TargetDate)
	}

	badEngine := NewEngine(sc, loc, &fakeLLM{reply: "maaf saya tidak mengerti"})
	resBad, err := badEngine.Process(ctx, "jadwal tanggal berapa ya")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resBad.Entities.TargetDay != 0 {
		t.Errorf("invalid LLM reply should not change target day, got %d", resBad.Entities.TargetDay)
	}
}

func TestBugisWordsPreserved(t *testing.T) {
	sc := NewSpellChecker()
	tests := []struct {
		input    string
		expected string
	}{
		{input: "aga kareba", expected: "aga kareba"},
		{input: "tegaki monro mandre", expected: "tegaki monro mandre"},
		{input: "purani mandre", expected: "purani mandre"},
	}
	for _, tc := range tests {
		got, _ := sc.CorrectSentence(tc.input)
		if got != tc.expected {
			t.Errorf("CorrectSentence(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}
