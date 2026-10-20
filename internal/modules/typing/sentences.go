package typing

import (
	"fmt"
	"math/rand"
	"unicode"
)

var (
	timePhrases = []string{
		"hari ini",
		"besok pagi",
		"nanti sore",
		"minggu depan",
		"setelah makan siang",
		"sebelum tidur",
		"akhir pekan ini",
	}
	activities = []string{
		"belajar",
		"mengerjakan tugas",
		"membaca materi",
		"ngoding",
		"diskusi kelompok",
		"mereview catatan",
		"latihan soal",
	}
	places = []string{
		"perpustakaan",
		"lab komputer",
		"kantin kampus",
		"kos",
		"ruang kelas",
		"warkop dekat kampus",
	}
	courses = []string{
		"algoritma",
		"basis data",
		"jaringan komputer",
		"kecerdasan buatan",
		"pemrograman web",
		"statistika",
		"sistem operasi",
	}
	reasons = []string{
		"deadline sudah dekat",
		"besok ada kuis",
		"dosennya galak",
		"materinya belum paham",
		"teman sekelas butuh bantuan",
	}
	sources = []string{
		"asisten dosen",
		"kakak tingkat",
		"modul praktikum",
		"grup kelas",
		"dosen pembimbing",
	}
	subjects = []string{
		"mahasiswa",
		"kita",
		"aku",
		"teman sekelas",
		"anak informatika",
	}
	habits = []string{
		"rajin belajar",
		"saling membantu",
		"mengerjakan tugas tepat waktu",
		"hadir lebih awal",
		"mencatat materi dengan rapi",
	}
	goals = []string{
		"nilainya bagus",
		"tugasnya cepat selesai",
		"tidak panik saat ujian",
		"presentasinya lancar",
		"waktunya lebih efisien",
	}
	conditions = []string{
		"materinya susah",
		"deadline mepet",
		"jaringan lancar",
		"dosennya berhalangan",
		"labnya kosong",
	}
	assignments = []string{
		"makalah",
		"laporan praktikum",
		"proyek kelompok",
		"latihan soal",
		"presentasi",
	}
	traits = []string{
		"rajin",
		"kreatif",
		"teliti",
		"santai",
		"sibuk",
	}
	easyReading = []string{
		"buku",
		"novel",
		"koran",
		"catatan",
		"materi",
	}
	easyWriting = []string{
		"catatan",
		"tugas",
		"surat",
	}
	easyPlaces = []string{
		"kelas",
		"kamar",
		"kos",
		"kampus",
		"taman",
		"perpustakaan",
		"kantin",
	}
	easyObjects = []string{
		"buku",
		"pena",
		"tas",
		"botol",
		"payung",
		"jaket",
	}
	easyDrinks = []string{
		"air",
		"teh",
		"kopi",
		"susu",
		"jus",
	}
	easyStudies = []string{
		"matematika",
		"fisika",
		"biologi",
		"sejarah",
		"kimia",
	}
	easyPartners = []string{
		"teman",
		"kakak",
		"adik",
		"sepupu",
	}
	assignmentSets = []string{
		"makalah, laporan praktikum, dan presentasi",
		"ringkasan materi, latihan soal, dan daftar pustaka",
		"abstrak, pembahasan, dan kesimpulan",
		"proposal, jadwal kegiatan, dan anggaran",
	}
	secondActivities = []string{
		"membahas soal",
		"menyusun laporan",
		"berlatih presentasi",
		"membagi tugas",
		"mengecek catatan",
	}
	people = []string{
		"teman sekelas",
		"kakak tingkat",
		"asisten dosen",
		"anggota kelompok",
	}
	hardReasons = []string{
		"waktunya makin sempit",
		"materinya belum dikuasai",
		"presentasinya sudah dekat",
		"teman kelompoknya belum selesai",
	}
	negatives = []string{
		"panik",
		"menunda pekerjaan",
		"begadang",
		"meminta perpanjangan",
	}
)

var sentenceTemplates = map[Difficulty][]func(r *rand.Rand) string{
	DifficultyEasy: {
		func(r *rand.Rand) string {
			return fmt.Sprintf("Aku membaca %s di %s.", pick(r, easyReading), pick(r, easyPlaces))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Dia menulis %s di %s.", pick(r, easyWriting), pick(r, easyPlaces))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Besok kita pergi ke %s.", pick(r, easyPlaces))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Jangan lupa bawa %s ke %s.", pick(r, easyObjects), pick(r, easyPlaces))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Aku minum %s setiap pagi.", pick(r, easyDrinks))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Kita belajar %s bersama %s.", pick(r, easyStudies), pick(r, easyPartners))
		},
	},
	DifficultyNormal: {
		func(r *rand.Rand) string {
			return fmt.Sprintf("%s aku %s di %s sebelum kelas %s dimulai.",
				pick(r, timePhrases), pick(r, activities), pick(r, places), pick(r, courses))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Jangan lupa %s %s karena %s.",
				pick(r, activities), pick(r, timePhrases), pick(r, reasons))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Menurut %s, %s harus %s supaya %s.",
				pick(r, sources), pick(r, subjects), pick(r, habits), pick(r, goals))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Kalau %s, kita %s di %s %s.",
				pick(r, conditions), pick(r, activities), pick(r, places), pick(r, timePhrases))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Tugas %s minggu ini adalah %s yang harus dikumpulkan %s.",
				pick(r, courses), pick(r, assignments), pick(r, timePhrases))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("%s yang %s biasanya %s di %s.",
				pick(r, subjects), pick(r, traits), pick(r, habits), pick(r, places))
		},
	},
	DifficultyHard: {
		func(r *rand.Rand) string {
			return fmt.Sprintf("Menurut %s, %s harus %s di %s supaya %s tanpa harus %s.",
				pick(r, sources), pick(r, subjects), pick(r, habits), pick(r, places), pick(r, goals), pick(r, negatives))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Sebelum kelas %s dimulai, %s biasanya %s di %s, lalu %s bersama %s sampai %s.",
				pick(r, courses), pick(r, subjects), pick(r, activities), pick(r, places), pick(r, secondActivities), pick(r, people), pick(r, timePhrases))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Tugas %s yang dikumpulkan %s harus memuat %s; jangan lupa menuliskan sumbernya dengan jelas.",
				pick(r, courses), pick(r, timePhrases), pick(r, assignmentSets))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Kalau %s, %s sebaiknya %s di %s sambil %s, karena %s dan %s.",
				pick(r, conditions), pick(r, subjects), pick(r, habits), pick(r, places), pick(r, secondActivities), pick(r, reasons), pick(r, hardReasons))
		},
		func(r *rand.Rand) string {
			return fmt.Sprintf("Berdasarkan %s, %s yang %s akan %s sebelum %s, sehingga %s tanpa harus %s.",
				pick(r, sources), pick(r, subjects), pick(r, traits), pick(r, habits), pick(r, timePhrases), pick(r, goals), pick(r, negatives))
		},
	},
}

func (m *Manager) newSentence(difficulty Difficulty) string {
	templates := sentenceTemplates[difficulty]
	if len(templates) == 0 {
		templates = sentenceTemplates[DifficultyNormal]
	}
	template := templates[m.random.Intn(len(templates))]
	return capitalizeFirst(template(m.random))
}

func pick(r *rand.Rand, options []string) string {
	return options[r.Intn(len(options))]
}

func capitalizeFirst(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return text
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
