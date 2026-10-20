package reminder

import (
	"testing"
	"time"
)

func mustLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Makassar")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}
	return loc
}

func TestParseWhenSuccess(t *testing.T) {
	loc := mustLocation(t)
	monday := time.Date(2026, 10, 19, 10, 0, 0, 0, loc)
	afternoon := time.Date(2026, 10, 19, 13, 0, 0, 0, loc)
	morning := time.Date(2026, 10, 19, 6, 0, 0, 0, loc)

	cases := []struct {
		name    string
		now     time.Time
		kind    Kind
		input   string
		want    time.Time
		message string
	}{
		{
			name: "duration minutes", now: monday, kind: KindReminder,
			input: "30 menit lagi minum obat",
			want:  time.Date(2026, 10, 19, 10, 30, 0, 0, loc), message: "minum obat",
		},
		{
			name: "duration hours minutes", now: monday, kind: KindReminder,
			input: "1 jam 30 menit lagi rapat",
			want:  time.Date(2026, 10, 19, 11, 30, 0, 0, loc), message: "rapat",
		},
		{
			name: "duration days", now: monday, kind: KindReminder,
			input: "2 hari lagi bayar ukt",
			want:  time.Date(2026, 10, 21, 10, 0, 0, 0, loc), message: "bayar ukt",
		},
		{
			name: "tomorrow with clock time first", now: monday, kind: KindReminder,
			input: "besok jam 8 kumpul tugas",
			want:  time.Date(2026, 10, 20, 8, 0, 0, 0, loc), message: "kumpul tugas",
		},
		{
			name: "tomorrow with clock time last", now: monday, kind: KindReminder,
			input: "kumpul tugas besok jam 8",
			want:  time.Date(2026, 10, 20, 8, 0, 0, 0, loc), message: "kumpul tugas",
		},
		{
			name: "next monday when today is monday", now: monday, kind: KindReminder,
			input: "senin jam 9 kuliah",
			want:  time.Date(2026, 10, 26, 9, 0, 0, 0, loc), message: "kuliah",
		},
		{
			name: "monday next week", now: monday, kind: KindReminder,
			input: "senin depan kuis",
			want:  time.Date(2026, 10, 26, 8, 0, 0, 0, loc), message: "kuis",
		},
		{
			name: "deadline slash date", now: monday, kind: KindDeadline,
			input: "25/10 23:59 tugas hukum",
			want:  time.Date(2026, 10, 25, 23, 59, 0, 0, loc), message: "tugas hukum",
		},
		{
			name: "month name with year omitted", now: monday, kind: KindReminder,
			input: "25 oktober jam 10 ujian",
			want:  time.Date(2026, 10, 25, 10, 0, 0, 0, loc), message: "ujian",
		},
		{
			name: "month name with tanggal filler", now: monday, kind: KindReminder,
			input: "tanggal 1 november jam 9 presentasi",
			want:  time.Date(2026, 11, 1, 9, 0, 0, 0, loc), message: "presentasi",
		},
		{
			name: "iso date", now: monday, kind: KindReminder,
			input: "2026-12-31 10:00 pesta",
			want:  time.Date(2026, 12, 31, 10, 0, 0, 0, loc), message: "pesta",
		},
		{
			name: "dash date", now: monday, kind: KindReminder,
			input: "25-12-2026 08:30 natal",
			want:  time.Date(2026, 12, 25, 8, 30, 0, 0, loc), message: "natal",
		},
		{
			name: "clock only rolls to evening", now: afternoon, kind: KindReminder,
			input: "jam 8 telepon ibu",
			want:  time.Date(2026, 10, 19, 20, 0, 0, 0, loc), message: "telepon ibu",
		},
		{
			name: "clock only stays in the morning", now: morning, kind: KindReminder,
			input: "jam 8 telepon ibu",
			want:  time.Date(2026, 10, 19, 8, 0, 0, 0, loc), message: "telepon ibu",
		},
		{
			name: "today with clock rolls to evening", now: afternoon, kind: KindReminder,
			input: "hari ini jam 8 rapat",
			want:  time.Date(2026, 10, 19, 20, 0, 0, 0, loc), message: "rapat",
		},
		{
			name: "nanti sore", now: monday, kind: KindReminder,
			input: "nanti sore jam 4 beli obat",
			want:  time.Date(2026, 10, 19, 16, 0, 0, 0, loc), message: "beli obat",
		},
		{
			name: "jam 8 malam", now: afternoon, kind: KindReminder,
			input: "jam 8 malam telepon",
			want:  time.Date(2026, 10, 19, 20, 0, 0, 0, loc), message: "telepon",
		},
		{
			name: "besok pagi period default", now: monday, kind: KindReminder,
			input: "besok pagi senam",
			want:  time.Date(2026, 10, 20, 8, 0, 0, 0, loc), message: "senam",
		},
		{
			name: "deadline day only uses end of day", now: monday, kind: KindDeadline,
			input: "besok kumpul tugas",
			want:  time.Date(2026, 10, 20, 23, 59, 0, 0, loc), message: "kumpul tugas",
		},
		{
			name: "reminder day only uses morning", now: monday, kind: KindReminder,
			input: "besok olahraga",
			want:  time.Date(2026, 10, 20, 8, 0, 0, 0, loc), message: "olahraga",
		},
		{
			name: "next week phrase", now: monday, kind: KindDeadline,
			input: "minggu depan presentasi",
			want:  time.Date(2026, 10, 26, 23, 59, 0, 0, loc), message: "presentasi",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseWhen(tc.input, tc.now, tc.kind)
			if err != nil {
				t.Fatalf("ParseWhen(%q) error: %v", tc.input, err)
			}
			if !got.Due.Equal(tc.want) {
				t.Errorf("due = %s, want %s", got.Due.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
			if got.Message != tc.message {
				t.Errorf("message = %q, want %q", got.Message, tc.message)
			}
		})
	}
}

func TestParseWhenErrors(t *testing.T) {
	loc := mustLocation(t)
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, loc)

	cases := []struct {
		name  string
		kind  Kind
		input string
	}{
		{name: "no time expression", kind: KindReminder, input: "kumpul tugas"},
		{name: "missing message", kind: KindReminder, input: "besok"},
		{name: "missing deadline message", kind: KindDeadline, input: "besok jam 8"},
		{name: "past day", kind: KindReminder, input: "kemarin rapat"},
		{name: "invalid clock", kind: KindReminder, input: "jam 27 makan"},
		{name: "empty input", kind: KindReminder, input: "   "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseWhen(tc.input, now, tc.kind); err == nil {
				t.Fatalf("ParseWhen(%q) expected error, got nil", tc.input)
			}
		})
	}
}
