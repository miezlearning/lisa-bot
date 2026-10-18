package schedule

import (
	"regexp"
	"strings"

	"whatsapp-bot/internal/database"
)

var (
	dayRegex   = regexp.MustCompile(`(?i)^\s*[*_~]?\s*(senin|selasa|rabu|kamis|jumat|sabtu|minggu)\s*[*_~]?:?\s*$`)
	timeRegex  = regexp.MustCompile(`(?i)^\s*[*_~]?\s*([0-2]?[0-9][.:][0-5][0-9])\s*[-–—]\s*([0-2]?[0-9][.:][0-5][0-9])\s*[*_~]?\s*$`)
	entryRegex = regexp.MustCompile(`^(.*?)(?:\s*\((.*?)\))?(?:\s*[-–—]\s*(.*?))?$`)

	dayNameMap = map[string]int{
		"senin":  1,
		"selasa": 2,
		"rabu":   3,
		"kamis":  4,
		"jumat":  5,
		"sabtu":  6,
		"minggu": 7,
	}

	intToDayName = map[int]string{
		1: "Senin",
		2: "Selasa",
		3: "Rabu",
		4: "Kamis",
		5: "Jumat",
		6: "Sabtu",
		7: "Minggu",
	}
)

// ParseRawSchedule parses unstructured raw WhatsApp text into a slice of database.Schedule records.
func ParseRawSchedule(rawText string) []database.Schedule {
	lines := strings.Split(rawText, "\n")
	var result []database.Schedule

	currentDayInt := 0
	currentDayName := ""
	currentStartTime := ""
	currentEndTime := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// 1. Check if line is a Day Header
		if match := dayRegex.FindStringSubmatch(trimmed); len(match) > 1 {
			dayLower := strings.ToLower(match[1])
			if dayInt, ok := dayNameMap[dayLower]; ok {
				currentDayInt = dayInt
				currentDayName = intToDayName[dayInt]
				currentStartTime = ""
				currentEndTime = ""
				continue
			}
		}

		// 2. Check if line is a Time Range (_07.30 - 09.10_)
		if match := timeRegex.FindStringSubmatch(trimmed); len(match) > 2 {
			currentStartTime = normalizeTime(match[1])
			currentEndTime = normalizeTime(match[2])
			continue
		}

		// 3. Otherwise, line is a Subject / Room / Note entry
		if currentDayInt > 0 {
			cleanLine := strings.Trim(trimmed, "*_~")
			matches := entryRegex.FindStringSubmatch(cleanLine)

			subject := ""
			room := ""
			notes := ""

			if len(matches) > 1 {
				subject = strings.TrimSpace(matches[1])
			}
			if len(matches) > 2 && matches[2] != "" {
				room = strings.TrimSpace(matches[2])
			}
			if len(matches) > 3 && matches[3] != "" {
				notes = strings.TrimSpace(matches[3])
			}

			if subject != "" {
				// Normalize room prefix
				if room != "" && !strings.HasPrefix(strings.ToLower(room), "ruang") {
					room = "Ruang " + room
				}

				// If Friday or flexible entry with no explicit time
				if currentStartTime == "" && strings.EqualFold(currentDayName, "Jumat") && notes == "" {
					notes = "Waktu & ruangan menyesuaikan"
				}

				sched := database.Schedule{
					DayOfWeek: currentDayInt,
					DayName:   currentDayName,
					StartTime: currentStartTime,
					EndTime:   currentEndTime,
					Subject:   subject,
					Room:      room,
					Notes:     notes,
				}
				result = append(result, sched)

				// Reset time for next item unless explicitly repeated
				currentStartTime = ""
				currentEndTime = ""
			}
		}
	}

	return result
}

func normalizeTime(t string) string {
	t = strings.ReplaceAll(t, ".", ":")
	parts := strings.Split(t, ":")
	if len(parts) == 2 {
		h := strings.TrimSpace(parts[0])
		m := strings.TrimSpace(parts[1])
		if len(h) == 1 {
			h = "0" + h
		}
		if len(m) == 1 {
			m = "0" + m
		}
		return h + ":" + m
	}
	return t
}
