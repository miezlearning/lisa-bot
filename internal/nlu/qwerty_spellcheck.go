package nlu

import (
	"math"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// QWERTY keyboard adjacency map for lowercase English characters.
var qwertyNeighbors = map[rune]string{
	'a': "qwsz",
	'b': "vghn",
	'c': "xdfv",
	'd': "serfcx",
	'e': "wsdr",
	'f': "drtgvc",
	'g': "ftyhbv",
	'h': "gyujnb",
	'i': "ujko",
	'j': "huikmn",
	'k': "jiolm",
	'l': "kop",
	'm': "njk",
	'n': "bhjm",
	'o': "iklp",
	'p': "ol",
	'q': "wa",
	'r': "edft",
	's': "awedxz",
	't': "rfgy",
	'u': "yhji",
	'v': "cfgb",
	'w': "qase",
	'x': "zsdc",
	'y': "tghu",
	'z': "asx",
}

// SpellChecker provides QWERTY-weighted spell correction, slang expansion, and vocabulary management.
type SpellChecker struct {
	mu         sync.RWMutex
	vocabulary map[string]bool
	slangMap   map[string]string
}

// NewSpellChecker initializes a SpellChecker with standard conversational vocabulary and Indonesian slang mappings.
func NewSpellChecker() *SpellChecker {
	sc := &SpellChecker{
		vocabulary: make(map[string]bool),
		slangMap:   make(map[string]string),
	}

	// Base Indonesian conversational vocabulary
	baseWords := []string{
		"itu", "ini", "ada", "apa", "mau", "coba", "kaya", "kayak", "disebutin", "sebutin",
		"larangan", "aturan", "praktikum", "tugas", "jadwal", "hari", "besok", "lusa", "kemarin",
		"sekarang", "ruang", "jam", "kelas", "senin", "selasa", "rabu", "kamis", "jumat", "sabtu", "minggu",
		"kosong", "libur", "pisah", "kapan", "berapa", "siapa", "dimana", "kuliah", "masuk", "habis", "lisa", "ai",
		"setelah", "sebelum", "info", "catatan", "tambah", "simpan", "ganti", "pindah", "zoom", "online",
		"offline", "tatap", "muka", "dosen", "ujian", "kuis", "tata", "tertib", "bantuan", "menu", "matkul",
		"mata", "pelajaran", "terlambat", "laporan", "nilai", "cek", "tolong", "bisa", "jadwalkan", "pakai",
		"dipakai", "mulai", "selesai", "jurusan", "fakultas", "hukum", "negara", "indonesia", "islam",
		"agama", "pancasila", "kewarganegaraan", "bahasa", "pendidikan", "dasar", "alamiah", "ilmu", "pengantar",
		"admin", "grup", "group", "cabut", "hapus", "daftar", "izinkan", "izin", "akses", "nomor",
	}

	for _, w := range baseWords {
		sc.vocabulary[strings.ToLower(w)] = true
	}

	// Slang and colloquial abbreviations
	baseSlang := map[string]string{
		"bsk":    "besok",
		"bsok":   "besok",
		"ap":     "apa",
		"y":      "ya",
		"hr":     "hari",
		"skrg":   "sekarang",
		"skr":    "sekarang",
		"kpn":       "kapan",
		"brp":       "berapa",
		"jdwl":      "jadwal",
		"jwdl":      "jadwal",
		"jdw":       "jadwal",
		"jdwal":     "jadwal",
		"jadual":    "jadwal",
		"jdwalku":   "jadwal",
		"jadwalku":  "jadwal",
		"jdwlku":    "jadwal",
		"jadwalmu":  "jadwal",
		"jdwlmu":    "jadwal",
		"jadwalnya": "jadwal",
		"jdwlnya":   "jadwal",
		"jdwalnya":  "jadwal",
		"kelasku":   "kelas",
		"kelasnya":  "kelas",
		"matkulku":  "jadwal",
		"matkulnya": "jadwal",
		"kuliahku":  "kuliah",
		"kuliahnya": "kuliah",
		"tugasku":   "tugas",
		"tugasnya":  "tugas",
		"dosenku":   "dosen",
		"dosennya":  "dosen",
		"matkul":    "jadwal",
		"mk":        "jadwal",
		"snin":      "senin",
		"slasa":  "selasa",
		"sls":    "selasa",
		"rbu":    "rabu",
		"kmis":   "kamis",
		"kms":    "kamis",
		"jmat":   "jumat",
		"jmt":    "jumat",
		"pkn":    "pendidikan kewarganegaraan",
		"pai":    "pendidikan agama islam",
		"bindo":  "pendidikan bahasa indonesia",
		"sy":     "saya",
		"km":     "kamu",
		"jg":     "juga",
		"dgn":    "dengan",
		"krn":    "karena",
		"sdh":    "sudah",
		"udh":    "sudah",
		"blm":    "belum",
		"tp":     "tapi",
		"kl":     "kalau",
		"klo":    "kalau",
		"lg":     "lagi",
		"aja":    "saja",
		"bgt":    "banget",
	}

	for k, v := range baseSlang {
		sc.slangMap[k] = v
	}

	return sc
}

// LearnFromDatabaseTexts extracts words from database records to enrich vocabulary and generate course acronyms.
func (sc *SpellChecker) LearnFromDatabaseTexts(texts []string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	wordRegex := regexp.MustCompile(`[a-zA-Z0-9\.]+`)

	for _, text := range texts {
		tokens := wordRegex.FindAllString(text, -1)
		var validWords []string

		for _, token := range tokens {
			clean := strings.ToLower(token)
			// Retain room codes like b.204, a.304 directly
			if isRoomCode(clean) {
				sc.vocabulary[clean] = true
				continue
			}

			// Strip punctuation inside word
			clean = strings.Trim(clean, ".")
			if len(clean) >= 2 {
				sc.vocabulary[clean] = true
				if isAlphabetic(clean) {
					validWords = append(validWords, clean)
				}
			}
		}

		// Generate dynamic acronym if title contains multiple words (e.g. "Pengantar Ilmu Hukum" -> "pih")
		if len(validWords) >= 2 {
			var acronymBuilder strings.Builder
			for _, w := range validWords {
				// skip connecting words
				if w == "dan" || w == "di" || w == "ke" || w == "ruang" {
					continue
				}
				runes := []rune(w)
				if len(runes) > 0 {
					acronymBuilder.WriteRune(runes[0])
				}
			}
			acronym := acronymBuilder.String()
			// Only generate acronyms of 3+ letters and never override existing slang.
			// 2-letter acronyms (e.g. "ap" from "Aturan Praktikum") collide with common chat words.
			if len(acronym) >= 3 {
				if _, exists := sc.slangMap[acronym]; !exists {
					sc.slangMap[acronym] = strings.Join(validWords, " ")
				}
				sc.vocabulary[acronym] = true
			}
		}
	}
}

// AddWord manually registers a word to vocabulary.
func (sc *SpellChecker) AddWord(word string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.vocabulary[strings.ToLower(word)] = true
}

// AddSlang manually registers a slang translation.
func (sc *SpellChecker) AddSlang(slang, target string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.slangMap[strings.ToLower(slang)] = strings.ToLower(target)
}

// CleanMentions strips WhatsApp mention tags like @bot, @lisa, @AI Lisa, @628xxx, @12345xxx, etc.
func CleanMentions(text string) string {
	mentionRegex := regexp.MustCompile(`(?i)@(?:ai\s+lisa|lisa|bot|[a-zA-Z0-9_\.\-]+)`)
	cleaned := mentionRegex.ReplaceAllString(text, "")
	return strings.TrimSpace(cleaned)
}

// stripIndonesianClitic strips possessive and definite clitics (-nya/-ny, -ku, -mu) from Indonesian words.
func stripIndonesianClitic(w string) string {
	runes := []rune(w)
	if strings.HasSuffix(w, "nya") && len(runes) > 4 {
		return string(runes[:len(runes)-3])
	}
	// Common texting shorthand: -ny instead of -nya (e.g. "jdwlny", "jadwalny")
	if strings.HasSuffix(w, "ny") && len(runes) > 3 {
		return string(runes[:len(runes)-2])
	}
	if (strings.HasSuffix(w, "ku") || strings.HasSuffix(w, "mu")) && len(runes) > 3 {
		return string(runes[:len(runes)-2])
	}
	return w
}

// SquashRepeatingCharacters reduces any sequence of identical consecutive runes to a single rune.
func SquashRepeatingCharacters(s string) string {
	var b strings.Builder
	var last rune
	for i, r := range s {
		if i == 0 || r != last {
			b.WriteRune(r)
			last = r
		}
	}
	return b.String()
}

// CorrectSentence cleans, squashes, expands slang, and applies QWERTY-weighted fuzzy correction on each word.
func (sc *SpellChecker) CorrectSentence(input string) (string, []string) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	cleanedInput := CleanMentions(input)
	if strings.TrimSpace(cleanedInput) == "" {
		return "", nil
	}

	words := strings.Fields(cleanedInput)
	var correctedWords []string
	var corrections []string

	for _, rawWord := range words {
		lowerWord := strings.ToLower(rawWord)

		// Retain room codes like b.204 without modification
		if isRoomCode(lowerWord) {
			correctedWords = append(correctedWords, lowerWord)
			continue
		}

		// Separate trailing punctuation if any (like ?, !, .)
		prefix, wordCore, suffix := splitPunctuation(lowerWord)
		if wordCore == "" {
			correctedWords = append(correctedWords, rawWord)
			continue
		}

		// Step 1: Character Squashing (e.g. "besokkk" -> "besok")
		squashed := SquashRepeatingCharacters(wordCore)

		// Step 2: Slang & Synonym Map Lookup
		if replacement, ok := sc.slangMap[squashed]; ok {
			correctedWords = append(correctedWords, prefix+replacement+suffix)
			if squashed != replacement && rawWord != replacement {
				corrections = append(corrections, rawWord+" -> "+replacement)
			}
			continue
		}
		if replacement, ok := sc.slangMap[wordCore]; ok {
			correctedWords = append(correctedWords, prefix+replacement+suffix)
			if wordCore != replacement && rawWord != replacement {
				corrections = append(corrections, rawWord+" -> "+replacement)
			}
			continue
		}

		// Step 3: Exact Vocabulary Match
		if sc.vocabulary[squashed] {
			correctedWords = append(correctedWords, prefix+squashed+suffix)
			if wordCore != squashed {
				corrections = append(corrections, rawWord+" -> "+squashed)
			}
			continue
		}
		if sc.vocabulary[wordCore] {
			correctedWords = append(correctedWords, prefix+wordCore+suffix)
			continue
		}

		// Step 3b: Indonesian Enclitic Pronoun Stripping (-ku, -mu, -nya)
		stem := stripIndonesianClitic(squashed)
		if stem != squashed {
			if replacement, ok := sc.slangMap[stem]; ok {
				correctedWords = append(correctedWords, prefix+replacement+suffix)
				corrections = append(corrections, rawWord+" -> "+replacement)
				continue
			}
			if sc.vocabulary[stem] {
				correctedWords = append(correctedWords, prefix+stem+suffix)
				corrections = append(corrections, rawWord+" -> "+stem)
				continue
			}
		}

		// Step 4: QWERTY-Weighted Levenshtein Fuzzy Search
		targetCandidate := squashed
		bestWord, bestDist := sc.findBestMatch(targetCandidate)

		wordLen := len([]rune(targetCandidate))
		threshold := 0.0

		if wordLen >= 3 && wordLen <= 4 {
			threshold = 0.65
		} else if wordLen >= 5 {
			threshold = 1.40
		}

		if threshold > 0.0 && bestDist <= threshold && bestWord != "" {
			correctedWords = append(correctedWords, prefix+bestWord+suffix)
			corrections = append(corrections, rawWord+" -> "+bestWord)
		} else {
			// Keep squashed or original word if no confident correction found
			correctedWords = append(correctedWords, prefix+squashed+suffix)
		}
	}

	result := strings.Join(correctedWords, " ")
	return result, corrections
}

// findBestMatch searches the vocabulary for the word with lowest QWERTY-weighted Damerau-Levenshtein distance.
func (sc *SpellChecker) findBestMatch(word string) (string, float64) {
	bestWord := ""
	minDist := math.MaxFloat64
	wRunes := []rune(word)
	wLen := len(wRunes)

	for vocab := range sc.vocabulary {
		vRunes := []rune(vocab)
		vLen := len(vRunes)

		// Quick length filter
		if int(math.Abs(float64(wLen-vLen))) > 2 {
			continue
		}

		dist := QwertyDamerauLevenshtein(wRunes, vRunes)
		if dist < minDist {
			minDist = dist
			bestWord = vocab
		} else if dist == minDist && minDist < math.MaxFloat64 {
			// Prefer candidate whose length is closer to target
			if int(math.Abs(float64(vLen-wLen))) < int(math.Abs(float64(len([]rune(bestWord))-wLen))) {
				bestWord = vocab
			}
		}
	}

	return bestWord, minDist
}

// QwertyDamerauLevenshtein calculates weighted edit distance:
// - Same character: 0.0
// - Adjacent QWERTY key: 0.35
// - Non-adjacent substitution: 1.00
// - Adjacent transposition: 0.50
// - Insertion / deletion: 1.00
func QwertyDamerauLevenshtein(s1, s2 []rune) float64 {
	n := len(s1)
	m := len(s2)

	if n == 0 {
		return float64(m)
	}
	if m == 0 {
		return float64(n)
	}

	dp := make([][]float64, n+1)
	for i := range dp {
		dp[i] = make([]float64, m+1)
		dp[i][0] = float64(i)
	}
	for j := 0; j <= m; j++ {
		dp[0][j] = float64(j)
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			r1 := s1[i-1]
			r2 := s2[j-1]

			var subCost float64
			if r1 == r2 {
				subCost = 0.0
			} else if isQwertyNeighbor(r1, r2) {
				subCost = 0.35
			} else {
				subCost = 1.00
			}

			costMatch := dp[i-1][j-1] + subCost
			costDel := dp[i-1][j] + 1.00
			costIns := dp[i][j-1] + 1.00

			minCost := costMatch
			if costDel < minCost {
				minCost = costDel
			}
			if costIns < minCost {
				minCost = costIns
			}

			// Transposition check
			if i > 1 && j > 1 && s1[i-1] == s2[j-2] && s1[i-2] == s2[j-1] {
				transCost := dp[i-2][j-2] + 0.50
				if transCost < minCost {
					minCost = transCost
				}
			}

			dp[i][j] = minCost
		}
	}

	return dp[n][m]
}

func isQwertyNeighbor(r1, r2 rune) bool {
	if neighbors, ok := qwertyNeighbors[r1]; ok {
		return strings.ContainsRune(neighbors, r2)
	}
	return false
}

func isRoomCode(s string) bool {
	// Matches codes like "b.204", "a.305", "r.101"
	if len(s) == 5 && unicode.IsLetter(rune(s[0])) && s[1] == '.' &&
		unicode.IsDigit(rune(s[2])) && unicode.IsDigit(rune(s[3])) && unicode.IsDigit(rune(s[4])) {
		return true
	}
	return false
}

func isAlphabetic(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func splitPunctuation(s string) (prefix, core, suffix string) {
	runes := []rune(s)
	start := 0
	end := len(runes)

	for start < end && !unicode.IsLetter(runes[start]) && !unicode.IsDigit(runes[start]) {
		start++
	}
	for end > start && !unicode.IsLetter(runes[end-1]) && !unicode.IsDigit(runes[end-1]) {
		end--
	}

	return string(runes[:start]), string(runes[start:end]), string(runes[end:])
}
