package nlu

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RelativeTimeType represents relative day/time identifiers.
type RelativeTimeType string

const (
	RelativeNone             RelativeTimeType = ""
	RelativeNow              RelativeTimeType = "now"
	RelativeToday            RelativeTimeType = "today"
	RelativeTomorrow         RelativeTimeType = "tomorrow"
	RelativeDayAfterTomorrow RelativeTimeType = "day_after_tomorrow"
	RelativeYesterday        RelativeTimeType = "yesterday"
)

// Entities contains all extracted parameters from a normalized user query.
type Entities struct {
	RelativeTime   RelativeTimeType
	TargetDay      int    // 1=Senin, ..., 7=Minggu (0 if unspecified)
	TargetDayName  string // "Senin", "Selasa", etc.
	TargetDate     string // "YYYY-MM-DD" in configured timezone
	Subject        string // e.g. "Pengantar Ilmu Hukum"
	Room           string // e.g. "Ruang B.204"
	KnowledgeTopic string // e.g. "larangan", "aturan"
	OverrideNote   string // notes for crowd-sourced override
	RawPayload     string // payload for commands like "simpan jadwal" or "tambah info"
}

// DayNameMap maps lowercase Indonesian day names to 1-7 (1=Senin..7=Minggu).
var DayNameMap = map[string]int{
	"senin":   1,
	"selasa":  2,
	"rabu":    3,
	"kamis":   4,
	"jumat":   5,
	"sabtu":   6,
	"minggu":  7,
	"ahad":    7,
}

var IntToDayName = map[int]string{
	1: "Senin",
	2: "Selasa",
	3: "Rabu",
	4: "Kamis",
	5: "Jumat",
	6: "Sabtu",
	7: "Minggu",
}

// ExtractEntities parses the normalized text and populates an Entities struct using the configured location.
func ExtractEntities(text string, loc *time.Location) *Entities {
	if loc == nil {
		loc = time.Local
	}
	now := time.Now().In(loc)
	lower := strings.ToLower(text)

	ent := &Entities{
		TargetDate: now.Format("2006-01-02"),
	}

	// 1. Detect Relative Time
	switch {
	case containsAny(lower, []string{"sekarang", "saat ini", "habis ini", "setelah ini"}):
		ent.RelativeTime = RelativeNow
		ent.TargetDay = GoWeekdayToScheduleDay(now.Weekday())
		ent.TargetDayName = IntToDayName[ent.TargetDay]
		ent.TargetDate = now.Format("2006-01-02")
	case containsAny(lower, []string{"hari ini", "hr ini"}):
		ent.RelativeTime = RelativeToday
		ent.TargetDay = GoWeekdayToScheduleDay(now.Weekday())
		ent.TargetDayName = IntToDayName[ent.TargetDay]
		ent.TargetDate = now.Format("2006-01-02")
	case containsAny(lower, []string{"besok", "bsk", "bsok"}):
		ent.RelativeTime = RelativeTomorrow
		tmrw := now.AddDate(0, 0, 1)
		ent.TargetDay = GoWeekdayToScheduleDay(tmrw.Weekday())
		ent.TargetDayName = IntToDayName[ent.TargetDay]
		ent.TargetDate = tmrw.Format("2006-01-02")
	case containsAny(lower, []string{"lusa"}):
		ent.RelativeTime = RelativeDayAfterTomorrow
		lusa := now.AddDate(0, 0, 2)
		ent.TargetDay = GoWeekdayToScheduleDay(lusa.Weekday())
		ent.TargetDayName = IntToDayName[ent.TargetDay]
		ent.TargetDate = lusa.Format("2006-01-02")
	case containsAny(lower, []string{"kemarin"}):
		ent.RelativeTime = RelativeYesterday
		kmrn := now.AddDate(0, 0, -1)
		ent.TargetDay = GoWeekdayToScheduleDay(kmrn.Weekday())
		ent.TargetDayName = IntToDayName[ent.TargetDay]
		ent.TargetDate = kmrn.Format("2006-01-02")
	}

	// 2. Detect Relative Day Offsets ("3 hari lagi", "2 hari lgi", "minggu depan")
	timeResolved := ent.RelativeTime != RelativeNone
	if !timeResolved {
		offset := -1

		dayOffsetRegex := regexp.MustCompile(`(?i)\b(\d{1,2})\s*(?:hari|hr)\s*(?:lagi|lgi|kedepan|ke\s+depan|kemudian)\b`)
		if m := dayOffsetRegex.FindStringSubmatch(lower); len(m) > 1 {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n <= 60 {
				offset = n
			}
		}

		if offset < 0 {
			nextWeekRegex := regexp.MustCompile(`(?i)\b(?:minggu|pekan)\s+depan\b`)
			if nextWeekRegex.MatchString(lower) {
				offset = 7
			}
		}

		if offset >= 0 {
			applyTargetDate(ent, now.AddDate(0, 0, offset))
			timeResolved = true
		}
	}

	// 3. Detect Explicit Day
	if !timeResolved {
		for dayName, dayInt := range DayNameMap {
			pattern := `\b` + regexp.QuoteMeta(dayName) + `\b`
			if matched, _ := regexp.MatchString(pattern, lower); matched {
				ent.TargetDay = dayInt
				ent.TargetDayName = IntToDayName[dayInt]
				break
			}
		}
	}

	// 4. Detect Room Code (e.g. "b.204", "a.304", "ruang b.204")
	roomRegex := regexp.MustCompile(`(?i)(?:ruang\s+)?([a-z]\.[0-9]{3})`)
	if roomMatch := roomRegex.FindStringSubmatch(text); len(roomMatch) > 1 {
		ent.Room = strings.ToUpper(roomMatch[1])
	}

	// 5. Detect Subject Names & Common Acronyms
	subjectAliases := map[string][]string{
		"Pengantar Ilmu Hukum":        {"pengantar ilmu hukum", "pih"},
		"Pengantar Hukum Indonesia":   {"pengantar hukum indonesia", "phi"},
		"Ilmu Negara":                 {"ilmu negara"},
		"Pendidikan Kewarganegaraan":  {"pendidikan kewarganegaraan", "pkn"},
		"Ilmu Alamiah Dasar":          {"ilmu alamiah dasar", "iad"},
		"Pend. Agama Islam":           {"pendidikan agama islam", "pend. agama islam", "pai", "agama islam"},
		"Pend. Bahasa Indonesia":      {"pendidikan bahasa indonesia", "pend. bahasa indonesia", "bindo", "bahasa indonesia"},
		"Pend. Pancasila":             {"pendidikan pancasila", "pend. pancasila", "pancasila"},
		"Praktikum Pend. Agama Islam": {"praktikum pend. agama islam", "praktikum agama", "praktikum pai"},
	}

	for canonical, aliases := range subjectAliases {
		for _, alias := range aliases {
			pattern := `\b` + regexp.QuoteMeta(alias) + `\b`
			if matched, _ := regexp.MatchString(pattern, lower); matched {
				ent.Subject = canonical
				break
			}
		}
		if ent.Subject != "" {
			break
		}
	}

	return ent
}

// applyTargetDate fills TargetDay, TargetDayName, and TargetDate from a concrete date.
func applyTargetDate(ent *Entities, t time.Time) {
	ent.TargetDay = GoWeekdayToScheduleDay(t.Weekday())
	ent.TargetDayName = IntToDayName[ent.TargetDay]
	ent.TargetDate = t.Format("2006-01-02")
}

// GoWeekdayToScheduleDay maps standard time.Weekday (Sunday=0..Saturday=6) to Monday=1..Sunday=7.
func GoWeekdayToScheduleDay(w time.Weekday) int {
	if w == time.Sunday {
		return 7
	}
	return int(w)
}

func containsAny(s string, keywords []string) bool {
	for _, kw := range keywords {
		pattern := `\b` + regexp.QuoteMeta(kw) + `\b`
		if matched, _ := regexp.MatchString(pattern, s); matched {
			return true
		}
	}
	return false
}
