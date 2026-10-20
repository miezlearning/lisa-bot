package typing

import (
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"kucing", "kucin", 1},
		{"abc", "abc", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"aku belajar", "aku beljar", 1},
	}
	for _, tc := range cases {
		if got := levenshtein([]rune(tc.a), []rune(tc.b)); got != tc.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestNormalizeAnswer(t *testing.T) {
	got := normalizeAnswer("  Hari   Ini AKU   belajar. ")
	want := "hari ini aku belajar."
	if got != want {
		t.Fatalf("normalizeAnswer = %q, want %q", got, want)
	}
}

func TestSentenceGeneration(t *testing.T) {
	m := NewManager()
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		sentence := m.newSentence(DifficultyNormal)
		runes := []rune(sentence)
		if len(runes) < 20 {
			t.Fatalf("sentence too short: %q", sentence)
		}
		if !unicode.IsUpper(runes[0]) {
			t.Fatalf("sentence not capitalized: %q", sentence)
		}
		if !strings.HasSuffix(sentence, ".") {
			t.Fatalf("sentence missing period: %q", sentence)
		}
		seen[sentence] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected sentence variety, got %d unique sentences", len(seen))
	}
}

func TestSentenceDifficultyLengths(t *testing.T) {
	m := NewManager()
	for i := 0; i < 200; i++ {
		easy := m.newSentence(DifficultyEasy)
		if n := len([]rune(easy)); n > 50 {
			t.Fatalf("easy sentence too long (%d): %q", n, easy)
		}
		hard := m.newSentence(DifficultyHard)
		if n := len([]rune(hard)); n < 80 {
			t.Fatalf("hard sentence too short (%d): %q", n, hard)
		}
	}
}

func TestDifficultySelection(t *testing.T) {
	m := NewManager()
	m.joinTimeout = time.Hour

	handled, reply, _ := m.HandleMessage("chat", "host", "Host", "!typing easy")
	if !handled || !strings.Contains(reply, "Easy") {
		t.Fatalf("create easy failed: handled=%v reply=%q", handled, reply)
	}

	m.mu.Lock()
	session := m.sessions["chat"]
	m.mu.Unlock()
	if session == nil || session.Difficulty != DifficultyEasy {
		t.Fatal("expected Easy session")
	}

	handled, reply, _ = m.HandleMessage("chat", "p2", "Budi", "!level hard")
	if !handled || !strings.Contains(reply, "Hanya pembuat sesi") {
		t.Fatalf("non-host level change should be rejected: handled=%v reply=%q", handled, reply)
	}

	handled, reply, _ = m.HandleMessage("chat", "host", "Host", "!level hard")
	if !handled || !strings.Contains(reply, "Hard") {
		t.Fatalf("host level change failed: handled=%v reply=%q", handled, reply)
	}

	m.mu.Lock()
	session = m.sessions["chat"]
	m.mu.Unlock()
	if session == nil || session.Difficulty != DifficultyHard {
		t.Fatal("expected Hard session after level change")
	}

	handled, reply, _ = m.HandleMessage("chat", "host", "Host", "!level extreme")
	if !handled || !strings.Contains(reply, "tidak dikenal") {
		t.Fatalf("invalid level should be rejected: handled=%v reply=%q", handled, reply)
	}

	handled, reply, _ = m.HandleMessage("chat2", "host", "Host", "!typing")
	if !handled || !strings.Contains(reply, "Normal") {
		t.Fatalf("default level should be Normal: handled=%v reply=%q", handled, reply)
	}

	handled, reply, _ = m.HandleMessage("chat3", "host", "Host", "!typing extreme")
	if !handled || !strings.Contains(reply, "tidak dikenal") {
		t.Fatalf("invalid create level should be rejected: handled=%v reply=%q", handled, reply)
	}
	m.mu.Lock()
	_, exists := m.sessions["chat3"]
	m.mu.Unlock()
	if exists {
		t.Fatal("invalid level should not create a session")
	}
}

func TestSessionFlow(t *testing.T) {
	m := NewManager()
	m.joinTimeout = time.Hour
	m.runTimeout = time.Hour

	handled, reply, markStarted := m.HandleMessage("chat", "host", "Host", "!typing")
	if !handled || markStarted {
		t.Fatalf("create: handled=%v markStarted=%v", handled, markStarted)
	}
	if !strings.Contains(reply, "Tes Mengetik") {
		t.Fatalf("create reply missing title: %q", reply)
	}

	handled, reply, _ = m.HandleMessage("chat", "p2", "Budi", "!join")
	if !handled || !strings.Contains(reply, "ikut") {
		t.Fatalf("join failed: handled=%v reply=%q", handled, reply)
	}

	handled, reply, _ = m.HandleMessage("chat", "host", "Host", "!mulai")
	if !handled || !strings.Contains(reply, "Dimulai") {
		t.Fatalf("start failed: handled=%v reply=%q", handled, reply)
	}

	m.MarkStarted("chat")

	if !m.IsParticipant("chat", "host") || !m.IsParticipant("chat", "p2") {
		t.Fatal("expected participants to be active before submitting")
	}

	m.mu.Lock()
	sentence := m.sessions["chat"].Sentence
	m.mu.Unlock()

	handled, reply, _ = m.HandleMessage("chat", "host", "Host", sentence)
	if !handled || !strings.Contains(reply, "Akurasi: 100.0%") {
		t.Fatalf("exact answer failed: handled=%v reply=%q", handled, reply)
	}
	if !strings.Contains(reply, "Progres: 1/2") {
		t.Fatalf("expected progress in reply, got %q", reply)
	}
	if m.IsParticipant("chat", "host") {
		t.Fatal("finished participant should no longer be intercepted")
	}

	handled, reply, _ = m.HandleMessage("chat", "p2", "Budi", "ini jawaban yang salah total")
	if !handled {
		t.Fatal("second answer should be handled")
	}
	if !strings.Contains(reply, "Hasil Tes Mengetik") {
		t.Fatalf("expected final leaderboard, got %q", reply)
	}
	if !strings.Contains(reply, "🥇") || strings.Index(reply, "Host") > strings.Index(reply, "Budi") {
		t.Fatalf("expected Host ranked first, got %q", reply)
	}

	if _, exists := m.sessions["chat"]; exists {
		t.Fatal("session should be removed after all participants finish")
	}

	handled, reply, _ = m.HandleMessage("chat", "host", "Host", "!leaderboard")
	if !handled || !strings.Contains(reply, "Hasil Tes Mengetik") {
		t.Fatalf("leaderboard command failed: handled=%v reply=%q", handled, reply)
	}
}

func TestBareCommandsDoNotHijackChat(t *testing.T) {
	m := NewManager()
	m.joinTimeout = time.Hour
	m.runTimeout = time.Hour

	for _, text := range []string{"mulai", "ikut", "batal", "skor", "!help", ".help", "#help"} {
		handled, _, _ := m.HandleMessage("chat", "u", "User", text)
		if handled {
			t.Errorf("%q should not be handled without an active session", text)
		}
	}
}

func TestCommandPrefixes(t *testing.T) {
	m := NewManager()
	m.joinTimeout = time.Hour

	for _, prefix := range []string{"!", "/", ".", "#"} {
		chat := "chat-" + prefix
		if handled, _, _ := m.HandleMessage(chat, "host", "Host", prefix+"typing"); !handled {
			t.Errorf("prefix %q should open a session", prefix)
		}
		if handled, _, _ := m.HandleMessage(chat, "host", "Host", prefix+"batal"); !handled {
			t.Errorf("prefix %q should cancel a session", prefix)
		}
	}
}

func TestRunTimeoutFinalizes(t *testing.T) {
	m := NewManager()
	m.joinTimeout = time.Hour
	m.runTimeout = 25 * time.Millisecond

	messages := make(chan string, 4)
	m.SetSender(func(chatJID, text string) {
		messages <- text
	})

	if handled, _, _ := m.HandleMessage("chat", "host", "Host", "!typing"); !handled {
		t.Fatal("create failed")
	}
	if handled, _, _ := m.HandleMessage("chat", "p2", "Budi", "!join"); !handled {
		t.Fatal("join failed")
	}
	if handled, _, mark := m.HandleMessage("chat", "host", "Host", "!mulai"); !handled || !mark {
		t.Fatal("start failed")
	}
	m.MarkStarted("chat")

	select {
	case msg := <-messages:
		if !strings.Contains(msg, "Waktu habis") {
			t.Fatalf("unexpected timeout message: %q", msg)
		}
		if !strings.Contains(msg, "Belum selesai: 2 peserta") {
			t.Fatalf("expected unfinished count, got %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run timeout did not fire")
	}

	if _, exists := m.sessions["chat"]; exists {
		t.Fatal("session should be removed after run timeout")
	}
}

func TestJoinTimeoutCancels(t *testing.T) {
	m := NewManager()
	m.joinTimeout = 25 * time.Millisecond
	m.runTimeout = time.Hour

	messages := make(chan string, 4)
	m.SetSender(func(chatJID, text string) {
		messages <- text
	})

	if handled, _, _ := m.HandleMessage("chat", "host", "Host", "!typing"); !handled {
		t.Fatal("create failed")
	}

	select {
	case msg := <-messages:
		if !strings.Contains(msg, "Waktu join habis") {
			t.Fatalf("unexpected join timeout message: %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("join timeout did not fire")
	}

	if _, exists := m.sessions["chat"]; exists {
		t.Fatal("session should be removed after join timeout")
	}
}
