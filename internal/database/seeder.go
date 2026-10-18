package database

import (
	"context"
	"fmt"
	"strings"

	"whatsapp-bot/internal/nlu"
)

// SeedInitialData populates initial schedules and knowledge base entries if empty.
func SeedInitialData(ctx context.Context, db *DB) error {
	var scheduleCount int
	err := db.Conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM schedules").Scan(&scheduleCount)
	if err != nil {
		return fmt.Errorf("failed to check schedules table: %w", err)
	}

	if scheduleCount == 0 {
		initialSchedules := []Schedule{
			// Senin
			{DayOfWeek: 1, DayName: "Senin", StartTime: "07:30", EndTime: "09:10", Subject: "Ilmu Negara", Room: "Ruang B.204", Notes: ""},
			{DayOfWeek: 1, DayName: "Senin", StartTime: "14:40", EndTime: "16:20", Subject: "Pengantar Hukum Indonesia", Room: "Ruang A.304", Notes: ""},

			// Selasa
			{DayOfWeek: 2, DayName: "Selasa", StartTime: "10:50", EndTime: "12:30", Subject: "Pendidikan Kewarganegaraan", Room: "Ruang B.204", Notes: ""},
			{DayOfWeek: 2, DayName: "Selasa", StartTime: "14:40", EndTime: "16:20", Subject: "Pengantar Ilmu Hukum", Room: "Ruang A.305", Notes: ""},

			// Rabu
			{DayOfWeek: 3, DayName: "Rabu", StartTime: "07:30", EndTime: "09:10", Subject: "Ilmu Alamiah Dasar", Room: "Ruang A.303", Notes: ""},
			{DayOfWeek: 3, DayName: "Rabu", StartTime: "13:00", EndTime: "14:40", Subject: "Pend. Agama Islam", Room: "Ruang A.305", Notes: ""},

			// Kamis
			{DayOfWeek: 4, DayName: "Kamis", StartTime: "07:30", EndTime: "09:10", Subject: "Pend. Bahasa Indonesia", Room: "Ruang B.204", Notes: ""},
			{DayOfWeek: 4, DayName: "Kamis", StartTime: "09:10", EndTime: "10:50", Subject: "Pend. Pancasila", Room: "Ruang A.205", Notes: "pisah"},
			{DayOfWeek: 4, DayName: "Kamis", StartTime: "13:00", EndTime: "14:40", Subject: "Pengantar Ilmu Hukum", Room: "Ruang A.205", Notes: ""},

			// Jumat
			{DayOfWeek: 5, DayName: "Jumat", StartTime: "", EndTime: "", Subject: "Praktikum Pend. Agama Islam", Room: "", Notes: "Waktu & ruangan menyesuaikan"},
		}

		if err := db.ClearAndInsertSchedules(ctx, initialSchedules); err != nil {
			return fmt.Errorf("failed to seed initial schedules: %w", err)
		}
	}

	var knowledgeCount int
	err = db.Conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM knowledge_base").Scan(&knowledgeCount)
	if err != nil {
		return fmt.Errorf("failed to check knowledge_base table: %w", err)
	}

	if knowledgeCount == 0 {
		initialKnowledge := []KnowledgeItem{
			{
				Topic:    "larangan",
				Keywords: "larangan,aturan praktikum,praktikum,tata tertib,lab,laboratorium",
				Content: "📌 *Tata Tertib & Larangan Praktikum*\n\n" +
					"1. Dilarang terlambat lebih dari 15 menit.\n" +
					"2. Wajib mengenakan pakaian berkerah rapi (bukan kaos oblong) dan sepatu tertutup.\n" +
					"3. Dilarang makan, minum, dan merokok di dalam ruang kelas/laboratorium.\n" +
					"4. Menjaga ketenangan dan kebersihan sarana inventaris.\n" +
					"5. Laporan pendahuluan/tugas wajib diserahkan sebelum sesi dimulai.",
			},
			{
				Topic:    "aturan",
				Keywords: "aturan,peraturan,kuliah,akademik,kehadiran,absensi",
				Content: "📋 *Aturan Perkuliahan & Akademik*\n\n" +
					"1. Kehadiran minimal 75% dari total pertemuan tatap muka/online sebagai syarat mengikuti Ujian Akhir Semester (UAS).\n" +
					"2. Toleransi keterlambatan maksimal 15 menit dari jam jadwal masuk.\n" +
					"3. Berpakaian rapi, sopan, dan bersepatu.\n" +
					"4. Ketidakhadiran karena sakit/keperluan mendesak wajib menyertakan surat izin resmi.",
			},
			{
				Topic:    "info bot",
				Keywords: "bot,bantuan,help,menu,panduan,fitur",
				Content: "🤖 *Panduan & Fitur Bot Lisa*\n\n" +
					"Perintah & pertanyaan yang dapat kamu ajukan:\n" +
					"• *Jadwal Hari Tertentu:* `jadwal senin`, `kamis ada kelas apa?`\n" +
					"• *Jadwal Relatif:* `jadwal besok`, `hari ini kuliah apa`, `lusa ada matkul?`\n" +
					"• *Status Saat Ini:* `sekarang kelas apa?`, `habis ini masuk mana?`\n" +
					"• *Cari Matkul / Ruang:* `kapan kelas pih?`, `ruang B.204 dipakai kapan?`\n" +
					"• *Update Info Harian:* `@Bot info hari ini pih pindah zoom`\n" +
					"• *Tambah Pengetahuan:* `@Bot tambah info <topik> | <isi informasi>`\n" +
					"• *Simpan Jadwal Raw:* `@Bot simpan jadwal` diikuti teks jadwal WhatsApp.",
			},
		}

		for _, item := range initialKnowledge {
			if err := db.UpsertKnowledge(ctx, item.Topic, item.Keywords, item.Content); err != nil {
				return fmt.Errorf("failed to seed knowledge item %s: %w", item.Topic, err)
			}
		}
	}

	return nil
}

// HarvestVocabulary collects all textual data from database records and registers it into the SpellChecker.
func HarvestVocabulary(ctx context.Context, db *DB, sc *nlu.SpellChecker) error {
	schedules, err := db.GetAllSchedules(ctx)
	if err != nil {
		return fmt.Errorf("failed to load schedules for vocabulary: %w", err)
	}

	var texts []string
	for _, s := range schedules {
		if s.Subject != "" {
			texts = append(texts, s.Subject)
		}
		if s.Room != "" {
			texts = append(texts, s.Room)
		}
		if s.Notes != "" {
			texts = append(texts, s.Notes)
		}
	}

	knowledge, err := db.GetAllKnowledge(ctx)
	if err != nil {
		return fmt.Errorf("failed to load knowledge base for vocabulary: %w", err)
	}

	for _, k := range knowledge {
		if k.Topic != "" {
			texts = append(texts, k.Topic)
		}
		if k.Keywords != "" {
			for _, kw := range strings.Split(k.Keywords, ",") {
				texts = append(texts, strings.TrimSpace(kw))
			}
		}
		if k.Content != "" {
			texts = append(texts, k.Content)
		}
	}

	sc.LearnFromDatabaseTexts(texts)
	return nil
}

// SeedInitialAdmins registers initial administrators from configuration or environment.
func SeedInitialAdmins(ctx context.Context, db *DB, adminNumbers []string) error {
	for _, rawNum := range adminNumbers {
		clean := strings.TrimSpace(rawNum)
		if clean != "" {
			if err := db.AddAdmin(ctx, clean, "Admin Awal", "system"); err != nil {
				return fmt.Errorf("gagal seeding admin %s: %w", clean, err)
			}
		}
	}
	return nil
}
