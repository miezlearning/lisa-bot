package reminder

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"whatsapp-bot/internal/nlu"
)

// Kind distinguishes regular reminders from deadlines.
type Kind string

const (
	KindReminder Kind = "reminder"
	KindDeadline Kind = "deadline"
)

const (
	defaultReminderHour = 8
	defaultReminderMin  = 0
	defaultDeadlineHour = 23
	defaultDeadlineMin  = 59
)

// ParsedWhen is the result of parsing a natural-language time expression.
type ParsedWhen struct {
	Due     time.Time
	Message string
}

type span struct {
	start int
	end   int
}

var (
	durationCombinedRe = regexp.MustCompile(`(?i)(?:dalam\s+)?(\d{1,3})\s*jam\s*(\d{1,2})\s*(?:menit|mnt)(?:\s*(?:lagi|lgi))?`)
	durationRe         = regexp.MustCompile(`(?i)(?:dalam\s+)?(\d{1,3})\s*(menit|mnt|jam|hari|hr|minggu|pekan|bulan)(?:\s*(?:lagi|lgi|ke\s*depan|kedepan))?`)
	halfHourRe         = regexp.MustCompile(`(?i)\bsetengah\s+jam\b`)
	quarterHourRe      = regexp.MustCompile(`(?i)\bseperempat\s+jam\b`)
	soonRe             = regexp.MustCompile(`(?i)\bsebentar\s+lagi\b`)
	laterRe            = regexp.MustCompile(`(?i)\b(?:nanti|ntar)\b`)

	isoDateRe      = regexp.MustCompile(`\b(\d{4})-(\d{1,2})-(\d{1,2})\b`)
	slashDateRe    = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})(?:/(\d{2,4}))?\b`)
	dashDateRe     = regexp.MustCompile(`\b(\d{1,2})-(\d{1,2})-(\d{2,4})\b`)
	monthDateRe    = regexp.MustCompile(`(?i)\b(\d{1,2})\s+(januari|februari|maret|april|mei|juni|juli|agustus|september|oktober|november|desember|jan|feb|mar|apr|jun|jul|agu|agt|sep|okt|nov|des)\b(?:\s*(\d{4}))?`)
	weekAheadRe    = regexp.MustCompile(`(?i)\b(?:minggu|pekan)\s+depan\b`)
	weekdayAheadRe = regexp.MustCompile(`(?i)\b(senin|selasa|rabu|kamis|jumat|sabtu|minggu|ahad)\s+(?:minggu\s+depan|pekan\s+depan|depan)\b`)
	relativeDayRe  = regexp.MustCompile(`(?i)\b(hari\s+ini|hr\s+ini|besok|bsok|bsk|lusa|kemarin)\b`)
	weekdayRe      = regexp.MustCompile(`(?i)\b(senin|selasa|rabu|kamis|jumat|sabtu|minggu|ahad)\b`)

	clockRe     = regexp.MustCompile(`(?i)\b(?:jam|pukul)\s*(\d{1,2})(?:[:.](\d{2}))?\b`)
	bareClockRe = regexp.MustCompile(`\b(\d{1,2})[:.](\d{2})\b`)
	periodRe    = regexp.MustCompile(`(?i)\b(subuh|pagi|siang|sore|petang|malam)\b`)
	hmRe        = regexp.MustCompile(`^(\d{1,2})[:.](\d{2})$`)
)

var monthNumbers = map[string]int{
	"januari": 1, "jan": 1,
	"februari": 2, "feb": 2,
	"maret": 3, "mar": 3,
	"april": 4, "apr": 4,
	"mei":  5,
	"juni": 6, "jun": 6,
	"juli": 7, "jul": 7,
	"agustus": 8, "agu": 8, "agt": 8,
	"september": 9, "sep": 9,
	"oktober": 10, "okt": 10,
	"november": 11, "nov": 11,
	"desember": 12, "des": 12,
}

var periodDefaults = map[string][2]int{
	"subuh":  {4, 30},
	"pagi":   {8, 0},
	"siang":  {12, 0},
	"sore":   {16, 0},
	"petang": {16, 0},
	"malam":  {19, 0},
}

var messageFillers = []string{
	"aku", "saya", "gue", "gua", "me", "untuk", "buat", "biar", "supaya",
	"tolong", "minta", "dong", "ya", "nih", "sih", "deh", "pada", "tanggal", "tgl",
}

// ParseWhen extracts a due time and the remaining free-form message from input.
func ParseWhen(input string, now time.Time, kind Kind) (ParsedWhen, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return ParsedWhen{}, fmt.Errorf("tulis pesan pengingatnya juga ya. Contoh: `!ingatkan besok jam 8 kumpul tugas`")
	}
	if now.IsZero() {
		now = time.Now()
	}
	loc := now.Location()
	lower := strings.ToLower(raw)

	var spans []span

	if d, sp, ok := findDuration(lower); ok {
		due := now.Add(d)
		if !due.After(now) {
			return ParsedWhen{}, fmt.Errorf("waktu yang kamu sebut sudah lewat. Coba sebutkan waktu lain ya")
		}
		spans = append(spans, sp)
		message := extractMessage(raw, spans)
		if message == "" {
			return ParsedWhen{}, missingMessageError(kind)
		}
		return ParsedWhen{Due: due, Message: message}, nil
	}

	date, dateSpan, hasDate := findDate(lower, now)
	clockHour, clockMinute, clockSpan, hasClock, invalidClock := findClock(lower)
	if invalidClock {
		return ParsedWhen{}, fmt.Errorf("jam yang kamu tulis tidak valid. Gunakan format 00-23, contoh `jam 8` atau `20:30`")
	}
	period, periodSpan, hasPeriod := findPeriod(lower)
	laterSpan, hasLater := findLater(lower)

	if !hasDate && !hasClock && !hasPeriod {
		if hasLater {
			due := now.Add(2 * time.Hour)
			spans = append(spans, laterSpan)
			message := extractMessage(raw, spans)
			if message == "" {
				return ParsedWhen{}, missingMessageError(kind)
			}
			return ParsedWhen{Due: due, Message: message}, nil
		}
		return ParsedWhen{}, fmt.Errorf("aku belum tahu kapan pengingatnya. Sebutkan waktu, contoh: `!ingatkan besok jam 8 kumpul tugas` atau `!ingatkan 2 jam lagi minum obat`")
	}

	targetDate := startOfDay(now)
	if hasDate {
		targetDate = date
		spans = append(spans, dateSpan)
	}

	hour, minute := 0, 0

	switch {
	case hasClock:
		hour, minute = clockHour, clockMinute
		spans = append(spans, clockSpan)
		if hasPeriod {
			hour = adjustHourWithPeriod(hour, period)
			spans = append(spans, periodSpan)
		}
	case hasPeriod:
		hour, minute = periodDefaults[period][0], periodDefaults[period][1]
		spans = append(spans, periodSpan)
	case hasDate:
		if kind == KindDeadline {
			hour, minute = defaultDeadlineHour, defaultDeadlineMin
		} else {
			hour, minute = defaultReminderHour, defaultReminderMin
		}
	}

	due := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), hour, minute, 0, 0, loc)

	if hasClock && !hasPeriod {
		if !hasDate || sameDate(targetDate, now) {
			due = resolveClock(now, targetDate, clockHour, clockMinute, !hasDate, loc)
		}
	}

	if !hasDate && !due.After(now) {
		due = due.AddDate(0, 0, 1)
	}

	if !due.After(now) {
		return ParsedWhen{}, fmt.Errorf("waktu yang kamu sebut sudah lewat. Coba sebutkan waktu lain ya")
	}

	if hasLater {
		spans = append(spans, laterSpan)
	}

	message := extractMessage(raw, spans)
	if message == "" {
		return ParsedWhen{}, missingMessageError(kind)
	}

	return ParsedWhen{Due: due, Message: message}, nil
}

func missingMessageError(kind Kind) error {
	if kind == KindDeadline {
		return fmt.Errorf("sebutkan juga judul deadline-nya. Contoh: `!deadline 25/10 23:59 tugas hukum`")
	}
	return fmt.Errorf("tulis pesan pengingatnya juga ya. Contoh: `!ingatkan besok jam 8 kumpul tugas`")
}

func findDuration(lower string) (time.Duration, span, bool) {
	if m := durationCombinedRe.FindStringSubmatchIndex(lower); m != nil {
		hours := atoi(lower[m[2]:m[3]])
		minutes := atoi(lower[m[4]:m[5]])
		d := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute
		if d > 0 {
			return d, span{m[0], m[1]}, true
		}
	}
	if m := halfHourRe.FindStringIndex(lower); m != nil {
		return 30 * time.Minute, span{m[0], m[1]}, true
	}
	if m := quarterHourRe.FindStringIndex(lower); m != nil {
		return 15 * time.Minute, span{m[0], m[1]}, true
	}
	if m := soonRe.FindStringIndex(lower); m != nil {
		return 15 * time.Minute, span{m[0], m[1]}, true
	}
	if m := durationRe.FindStringSubmatchIndex(lower); m != nil {
		n := atoi(lower[m[2]:m[3]])
		unit := lower[m[4]:m[5]]
		if n <= 0 {
			return 0, span{}, false
		}
		var d time.Duration
		switch unit {
		case "menit", "mnt":
			d = time.Duration(n) * time.Minute
		case "jam":
			d = time.Duration(n) * time.Hour
		case "hari", "hr":
			d = time.Duration(n) * 24 * time.Hour
		case "minggu", "pekan":
			d = time.Duration(n) * 7 * 24 * time.Hour
		case "bulan":
			d = time.Duration(n) * 30 * 24 * time.Hour
		}
		if d > 0 {
			return d, span{m[0], m[1]}, true
		}
	}
	return 0, span{}, false
}

func findDate(lower string, now time.Time) (time.Time, span, bool) {
	var best time.Time
	var bestSpan span
	found := false
	loc := now.Location()

	consider := func(start, end int, d time.Time) {
		if !found || start < bestSpan.start || (start == bestSpan.start && end > bestSpan.end) {
			best = d
			bestSpan = span{start, end}
			found = true
		}
	}

	if m := isoDateRe.FindStringSubmatchIndex(lower); m != nil {
		if d, ok := makeDate(atoi(lower[m[2]:m[3]]), atoi(lower[m[4]:m[5]]), atoi(lower[m[6]:m[7]]), loc); ok {
			consider(m[0], m[1], d)
		}
	}

	if m := slashDateRe.FindStringSubmatchIndex(lower); m != nil {
		day := atoi(lower[m[2]:m[3]])
		month := atoi(lower[m[4]:m[5]])
		year := now.Year()
		hasYear := m[6] >= 0
		if hasYear {
			year = atoi(lower[m[6]:m[7]])
			if year < 100 {
				year += 2000
			}
		}
		if d, ok := makeDate(year, month, day, loc); ok {
			if !hasYear && d.Before(startOfDay(now)) {
				d, _ = makeDate(year+1, month, day, loc)
			}
			consider(m[0], m[1], d)
		}
	}

	if m := dashDateRe.FindStringSubmatchIndex(lower); m != nil {
		year := atoi(lower[m[6]:m[7]])
		if year < 100 {
			year += 2000
		}
		if d, ok := makeDate(year, atoi(lower[m[4]:m[5]]), atoi(lower[m[2]:m[3]]), loc); ok {
			consider(m[0], m[1], d)
		}
	}

	if m := monthDateRe.FindStringSubmatchIndex(lower); m != nil {
		day := atoi(lower[m[2]:m[3]])
		month := monthNumbers[lower[m[4]:m[5]]]
		year := now.Year()
		hasYear := m[6] >= 0
		if hasYear {
			year = atoi(lower[m[6]:m[7]])
			if year < 100 {
				year += 2000
			}
		}
		if d, ok := makeDate(year, month, day, loc); ok {
			if !hasYear && d.Before(startOfDay(now)) {
				d, _ = makeDate(year+1, month, day, loc)
			}
			consider(m[0], m[1], d)
		}
	}

	if m := weekdayAheadRe.FindStringSubmatchIndex(lower); m != nil {
		matched := strings.TrimSpace(lower[m[0]:m[1]])
		if matched != "minggu depan" {
			if target, ok := weekdayFromName(lower[m[2]:m[3]]); ok {
				consider(m[0], m[1], nextWeekday(now, target, 1))
			}
		}
	}

	if m := weekAheadRe.FindStringIndex(lower); m != nil {
		consider(m[0], m[1], startOfDay(now).AddDate(0, 0, 7))
	}

	if m := relativeDayRe.FindStringSubmatchIndex(lower); m != nil {
		word := strings.Join(strings.Fields(lower[m[2]:m[3]]), " ")
		base := startOfDay(now)
		switch word {
		case "hari ini", "hr ini":
			consider(m[0], m[1], base)
		case "besok", "bsok", "bsk":
			consider(m[0], m[1], base.AddDate(0, 0, 1))
		case "lusa":
			consider(m[0], m[1], base.AddDate(0, 0, 2))
		case "kemarin":
			consider(m[0], m[1], base.AddDate(0, 0, -1))
		}
	}

	if m := weekdayRe.FindStringSubmatchIndex(lower); m != nil {
		if target, ok := weekdayFromName(lower[m[2]:m[3]]); ok {
			consider(m[0], m[1], nextWeekday(now, target, 0))
		}
	}

	return best, bestSpan, found
}

func findClock(lower string) (hour, minute int, sp span, found, invalid bool) {
	if m := clockRe.FindStringSubmatchIndex(lower); m != nil {
		h := atoi(lower[m[2]:m[3]])
		min := 0
		if m[4] >= 0 {
			min = atoi(lower[m[4]:m[5]])
		}
		if h > 23 || min > 59 {
			return 0, 0, span{}, false, true
		}
		return h, min, span{m[0], m[1]}, true, false
	}

	if m := bareClockRe.FindStringSubmatchIndex(lower); m != nil {
		h := atoi(lower[m[2]:m[3]])
		min := atoi(lower[m[4]:m[5]])
		if h <= 23 && min <= 59 {
			return h, min, span{m[0], m[1]}, true, false
		}
	}

	return 0, 0, span{}, false, false
}

func findPeriod(lower string) (string, span, bool) {
	m := periodRe.FindStringSubmatchIndex(lower)
	if m == nil {
		return "", span{}, false
	}
	return lower[m[2]:m[3]], span{m[0], m[1]}, true
}

func findLater(lower string) (span, bool) {
	m := laterRe.FindStringIndex(lower)
	if m == nil {
		return span{}, false
	}
	return span{m[0], m[1]}, true
}

func resolveClock(now, date time.Time, hour, minute int, rollover bool, loc *time.Location) time.Time {
	at := func(h int) time.Time {
		return time.Date(date.Year(), date.Month(), date.Day(), h, minute, 0, 0, loc)
	}

	if hour == 0 || hour == 12 {
		due := at(hour)
		if rollover && !due.After(now) {
			due = due.AddDate(0, 0, 1)
		}
		return due
	}

	candidates := []time.Time{at(hour)}
	if hour < 12 {
		candidates = append(candidates, at(hour+12))
	}
	for _, candidate := range candidates {
		if candidate.After(now) {
			return candidate
		}
	}

	if rollover {
		return at(hour).AddDate(0, 0, 1)
	}
	return at(hour)
}

func adjustHourWithPeriod(hour int, period string) int {
	switch period {
	case "subuh":
		if hour == 12 {
			return 0
		}
		if hour > 12 {
			return hour - 12
		}
	case "pagi":
		if hour == 12 {
			return 0
		}
		if hour > 12 {
			return hour - 12
		}
	case "siang":
		if hour >= 1 && hour <= 6 {
			return hour + 12
		}
	case "sore", "petang":
		if hour >= 1 && hour <= 6 {
			return hour + 12
		}
	case "malam":
		if hour == 12 {
			return 0
		}
		if hour >= 1 && hour <= 11 {
			return hour + 12
		}
	}
	return hour
}

func nextWeekday(now time.Time, target time.Weekday, weeksAhead int) time.Time {
	base := startOfDay(now)
	targetDay := int(target)
	if targetDay == 0 {
		targetDay = 7
	}

	if weeksAhead > 0 {
		currentMonday := base.AddDate(0, 0, -(nlu.GoWeekdayToScheduleDay(base.Weekday()) - 1))
		return currentMonday.AddDate(0, 0, weeksAhead*7+targetDay-1)
	}

	delta := (int(target) - int(base.Weekday()) + 7) % 7
	if delta == 0 {
		delta = 7
	}
	return base.AddDate(0, 0, delta)
}

func weekdayFromName(name string) (time.Weekday, bool) {
	day, ok := nlu.DayNameMap[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return time.Sunday, false
	}
	return time.Weekday(day % 7), true
}

func makeDate(year, month, day int, loc *time.Location) (time.Time, bool) {
	if year < 2000 || year > 2100 || month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, false
	}
	d := time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc)
	if d.Year() != year || int(d.Month()) != month || d.Day() != day {
		return time.Time{}, false
	}
	return d, true
}

func extractMessage(raw string, spans []span) string {
	sort.Slice(spans, func(i, j int) bool {
		return spans[i].start < spans[j].start
	})

	var b strings.Builder
	last := 0
	for _, sp := range spans {
		if sp.start < last || sp.start > len(raw) || sp.end > len(raw) {
			continue
		}
		b.WriteString(raw[last:sp.start])
		last = sp.end
	}
	b.WriteString(raw[last:])

	message := strings.Join(strings.Fields(b.String()), " ")
	message = strings.Trim(message, " -:,;.|/")
	message = trimFillers(message)
	return strings.TrimSpace(message)
}

func trimFillers(message string) string {
	for {
		changed := false
		lower := strings.ToLower(message)
		for _, filler := range messageFillers {
			if strings.HasPrefix(lower, filler+" ") {
				message = strings.TrimSpace(message[len(filler):])
				changed = true
				break
			}
			if strings.HasSuffix(lower, " "+filler) {
				message = strings.TrimSpace(message[:len(message)-len(filler)])
				changed = true
				break
			}
		}
		if !changed {
			return message
		}
	}
}

func parseTimeHM(value string) (hour, minute int, ok bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSpace(strings.TrimPrefix(value, "jam"))
	value = strings.TrimSpace(value)
	m := hmRe.FindStringSubmatch(value)
	if m == nil {
		return 0, 0, false
	}
	hour = atoi(m[1])
	minute = atoi(m[2])
	if hour > 23 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func sameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
