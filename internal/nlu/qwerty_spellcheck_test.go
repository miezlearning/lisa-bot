package nlu

import (
	"testing"
)

func TestQwertySpellcheck(t *testing.T) {
	sc := NewSpellChecker()

	// Train spellchecker with relevant context texts like in production
	sampleTexts := []string{
		"Pengantar Ilmu Hukum",
		"Pengantar Hukum Indonesia",
		"Ilmu Alamiah Dasar",
		"Pendidikan Kewarganegaraan",
		"Pendidikan Agama Islam",
		"Praktikum Tata Tertib dan Larangan di Ruang Lab",
		"Jadwal Kuliah Semester Ganjil",
	}
	sc.LearnFromDatabaseTexts(sampleTexts)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Neighbor QWERTY r to t",
			input:    "iru disebutin",
			expected: "itu disebutin",
		},
		{
			name:     "Case folding and neighbor v to b",
			input:    "MAU COVA",
			expected: "mau coba",
		},
		{
			name:     "Neighbor QWERTY f to g",
			input:    "itu kaya laranfan",
			expected: "itu kaya larangan",
		},
		{
			name:     "Neighbor QWERTY m to n",
			input:    "laramgan",
			expected: "larangan",
		},
		{
			name:     "Mention stripping and Indonesian slang expansion",
			input:    "@bot jdwl snin bsk",
			expected: "jadwal senin besok",
		},
		{
			name:     "Complex colloquial query with course acronym",
			input:    "jdwl snin bsk jam brp pih",
			expected: "jadwal senin besok jam berapa pengantar ilmu hukum",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, corrections := sc.CorrectSentence(tc.input)
			if got != tc.expected {
				t.Errorf("CorrectSentence(%q) = %q; want %q (corrections: %v)", tc.input, got, tc.expected, corrections)
			} else {
				t.Logf("PASS: %q -> %q (corrections: %v)", tc.input, got, corrections)
			}
		})
	}
}
