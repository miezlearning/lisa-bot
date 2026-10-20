package typing

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxParticipants    = 20
	defaultJoinTimeout = 10 * time.Minute
	defaultRunTimeout  = 5 * time.Minute
)

type sessionState int

const (
	stateJoining sessionState = iota
	statePreparing
	stateRunning
)

type Difficulty int

const (
	DifficultyEasy Difficulty = iota
	DifficultyNormal
	DifficultyHard
)

func (d Difficulty) String() string {
	switch d {
	case DifficultyEasy:
		return "Easy"
	case DifficultyHard:
		return "Hard"
	default:
		return "Normal"
	}
}

func (d Difficulty) Description() string {
	switch d {
	case DifficultyEasy:
		return "kata umum, kalimat pendek"
	case DifficultyHard:
		return "kalimat panjang + tanda baca"
	default:
		return "kalimat sehari-hari"
	}
}

type Participant struct {
	UserID   string
	Name     string
	Finished bool
	Elapsed  time.Duration
	Accuracy float64
	WPM      float64
}

type Session struct {
	ChatJID      string
	HostID       string
	State        sessionState
	Difficulty   Difficulty
	Sentence     string
	Participants map[string]*Participant
	Order        []string
	StartedAt    time.Time
	joinTimer    *time.Timer
	runTimer     *time.Timer
}

type command int

const (
	cmdNone command = iota
	cmdCreate
	cmdJoin
	cmdStart
	cmdCancel
	cmdLeaderboard
	cmdLevel
)

type Manager struct {
	mu          sync.Mutex
	sessions    map[string]*Session
	lastBoards  map[string]string
	sender      func(chatJID, text string)
	joinTimeout time.Duration
	runTimeout  time.Duration
	random      *rand.Rand
	now         func() time.Time
}

func NewManager() *Manager {
	return &Manager{
		sessions:    make(map[string]*Session),
		lastBoards:  make(map[string]string),
		joinTimeout: defaultJoinTimeout,
		runTimeout:  defaultRunTimeout,
		random:      rand.New(rand.NewSource(time.Now().UnixNano())),
		now:         time.Now,
	}
}

func (m *Manager) SetSender(sender func(chatJID, text string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sender = sender
}

func (m *Manager) IsParticipant(chatJID, userID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := m.sessions[chatJID]
	if session == nil || session.State != stateRunning {
		return false
	}
	participant := session.Participants[userID]
	return participant != nil && !participant.Finished
}

func (m *Manager) NeedsHandling(chatJID, userID, text string) bool {
	if m.IsParticipant(chatJID, userID) {
		return true
	}

	cmd, prefixed, _ := parseCommand(text)
	if cmd == cmdNone || prefixed {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	session := m.sessions[chatJID]
	if session == nil {
		return false
	}
	switch cmd {
	case cmdJoin:
		return session.State == stateJoining
	case cmdStart, cmdCancel:
		return session.HostID == userID
	case cmdLeaderboard:
		return true
	}
	return false
}

func (m *Manager) MarkStarted(chatJID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := m.sessions[chatJID]
	if session == nil || session.State != statePreparing {
		return
	}
	session.State = stateRunning
	session.StartedAt = m.now()
	session.runTimer = time.AfterFunc(m.runTimeout, func() {
		m.expireRun(chatJID, session)
	})
}

func (m *Manager) HandleMessage(chatJID, userID, userName, text string) (bool, string, bool) {
	cmd, prefixed, arg := parseCommand(text)
	switch cmd {
	case cmdCreate:
		return m.createSession(chatJID, userID, userName, arg)
	case cmdJoin:
		return m.joinSession(chatJID, userID, userName, prefixed)
	case cmdStart:
		return m.startSession(chatJID, userID, prefixed)
	case cmdCancel:
		return m.cancelSession(chatJID, userID, prefixed)
	case cmdLeaderboard:
		return m.showLeaderboard(chatJID, prefixed)
	case cmdLevel:
		return m.setDifficulty(chatJID, userID, arg, prefixed)
	}

	trimmed := strings.TrimSpace(text)
	if hasCommandPrefix(trimmed) {
		return false, "", false
	}
	return m.submitAnswer(chatJID, userID, text)
}

func hasCommandPrefix(text string) bool {
	if text == "" {
		return false
	}
	switch text[0] {
	case '!', '.', '/', '#':
		return true
	}
	return false
}

func parseCommand(text string) (command, bool, string) {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return cmdNone, false, ""
	}

	prefixed := false
	if hasCommandPrefix(lower) {
		prefixed = true
		lower = strings.TrimSpace(lower[1:])
	}
	lower = strings.Join(strings.Fields(lower), " ")
	if lower == "" {
		return cmdNone, false, ""
	}

	if rest, ok := matchPhrase(lower, "tes mengetik"); ok {
		return cmdCreate, prefixed, rest
	}
	if rest, ok := matchPhrase(lower, "typing test"); ok {
		return cmdCreate, prefixed, rest
	}

	fields := strings.Fields(lower)
	first := fields[0]
	rest := ""
	if len(fields) > 1 {
		rest = strings.Join(fields[1:], " ")
	}

	if prefixed {
		switch first {
		case "typing", "tt":
			return cmdCreate, true, rest
		case "level", "tingkat", "difficulty":
			return cmdLevel, true, rest
		}
	}

	switch lower {
	case "join", "ikut", "gabung":
		return cmdJoin, prefixed, ""
	case "mulai", "start", "go":
		return cmdStart, prefixed, ""
	case "batal", "cancel", "stop", "berhenti":
		return cmdCancel, prefixed, ""
	case "leaderboard", "lb", "skor", "papan skor":
		return cmdLeaderboard, prefixed, ""
	}
	return cmdNone, false, ""
}

func matchPhrase(text, phrase string) (string, bool) {
	if text == phrase {
		return "", true
	}
	if strings.HasPrefix(text, phrase+" ") {
		return strings.TrimSpace(strings.TrimPrefix(text, phrase)), true
	}
	return "", false
}

func parseDifficulty(arg string) (Difficulty, bool) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "", "normal", "sedang", "biasa", "n":
		return DifficultyNormal, true
	case "easy", "mudah", "gampang", "e":
		return DifficultyEasy, true
	case "hard", "sulit", "susah", "h":
		return DifficultyHard, true
	}
	return DifficultyNormal, false
}

func (m *Manager) createSession(chatJID, userID, userName, levelArg string) (bool, string, bool) {
	difficulty, valid := parseDifficulty(levelArg)
	if !valid {
		return true, fmt.Sprintf("⚠️ Level *%s* tidak dikenal.\nPilih: *easy*, *normal*, atau *hard*.\nContoh: !typing easy",
			strings.TrimSpace(levelArg)), false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if session, exists := m.sessions[chatJID]; exists {
		if session.State == stateJoining {
			return true, fmt.Sprintf("⚠️ Sudah ada sesi tes mengetik (Level: *%s*) di chat ini.\nKetik !join untuk ikut, atau !mulai kalau semua sudah siap.", session.Difficulty), false
		}
		return true, "⚠️ Tes mengetik sedang berjalan. Tunggu sampai selesai ya.", false
	}

	session := &Session{
		ChatJID:      chatJID,
		HostID:       userID,
		State:        stateJoining,
		Difficulty:   difficulty,
		Participants: make(map[string]*Participant),
	}
	session.addParticipant(userID, userName)
	m.sessions[chatJID] = session
	session.joinTimer = time.AfterFunc(m.joinTimeout, func() {
		m.expireJoin(chatJID, session)
	})

	reply := fmt.Sprintf("⌨️ *Tes Mengetik Lisa — %s*\n\nSesi dibuka oleh *%s*.\nLevel: *%s* (%s)\nKetik !join untuk ikut (maks %d peserta).\nHost ketik !mulai untuk memulai.\nGanti level: !level easy|normal|hard\n\n📊 Peserta: 1/%d\n⏳ Waktu join: %d menit.",
		difficulty, displayName(userName, userID), difficulty, difficulty.Description(), maxParticipants, maxParticipants, int(m.joinTimeout.Minutes()))
	return true, reply, false
}

func (m *Manager) joinSession(chatJID, userID, userName string, prefixed bool) (bool, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := m.sessions[chatJID]
	if session == nil {
		if !prefixed {
			return false, "", false
		}
		return true, "❌ Belum ada sesi tes mengetik. Ketik !typing untuk memulai.", false
	}
	if session.State != stateJoining {
		return true, "⏳ Tes sudah dimulai. Tunggu sesi berikutnya ya.", false
	}
	if _, exists := session.Participants[userID]; exists {
		return true, fmt.Sprintf("✅ Kamu sudah ikut. Peserta: %d/%d (Level: *%s*).\nHost ketik !mulai kalau semua siap.", len(session.Participants), maxParticipants, session.Difficulty), false
	}
	if len(session.Participants) >= maxParticipants {
		return true, fmt.Sprintf("❌ Peserta sudah penuh (%d orang).", maxParticipants), false
	}

	session.addParticipant(userID, userName)
	return true, fmt.Sprintf("✅ *%s* ikut! Peserta: %d/%d (Level: *%s*).\nHost ketik !mulai untuk memulai.", displayName(userName, userID), len(session.Participants), maxParticipants, session.Difficulty), false
}

func (m *Manager) startSession(chatJID, userID string, prefixed bool) (bool, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := m.sessions[chatJID]
	if session == nil {
		if !prefixed {
			return false, "", false
		}
		return true, "❌ Belum ada sesi tes mengetik. Ketik !typing untuk memulai.", false
	}
	if session.State != stateJoining {
		return true, "⏳ Tes sudah dimulai.", false
	}
	if session.HostID != userID {
		return true, "❌ Hanya pembuat sesi yang bisa memulai tes.", false
	}

	session.Sentence = m.newSentence(session.Difficulty)
	session.State = statePreparing
	if session.joinTimer != nil {
		session.joinTimer.Stop()
	}

	reply := fmt.Sprintf("🏁 *Tes Mengetik Dimulai — %s!*\n\nKetik kalimat ini *persis* seperti aslinya, lalu kirim di chat ini:\n\n\"%s\"\n\n⏱️ Waktu: %d menit\n👥 Peserta: %d",
		session.Difficulty, session.Sentence, int(m.runTimeout.Minutes()), len(session.Participants))
	return true, reply, true
}

func (m *Manager) cancelSession(chatJID, userID string, prefixed bool) (bool, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := m.sessions[chatJID]
	if session == nil {
		if !prefixed {
			return false, "", false
		}
		return true, "❌ Tidak ada sesi tes mengetik yang aktif.", false
	}
	if session.HostID != userID {
		return true, "❌ Hanya pembuat sesi yang bisa membatalkan.", false
	}
	if session.joinTimer != nil {
		session.joinTimer.Stop()
	}
	if session.runTimer != nil {
		session.runTimer.Stop()
	}
	delete(m.sessions, chatJID)
	return true, "🛑 Sesi tes mengetik dibatalkan.", false
}

func (m *Manager) setDifficulty(chatJID, userID, levelArg string, prefixed bool) (bool, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := m.sessions[chatJID]
	if session == nil {
		if !prefixed {
			return false, "", false
		}
		return true, "❌ Belum ada sesi tes mengetik. Ketik !typing untuk memulai.", false
	}
	if session.State != stateJoining {
		return true, "⏳ Level tidak bisa diubah setelah tes dimulai.", false
	}
	if session.HostID != userID {
		return true, "❌ Hanya pembuat sesi yang bisa mengubah level.", false
	}
	if strings.TrimSpace(levelArg) == "" {
		return true, fmt.Sprintf("ℹ️ Level saat ini: *%s* (%s).\nGanti dengan !level easy, !level normal, atau !level hard.", session.Difficulty, session.Difficulty.Description()), false
	}

	difficulty, valid := parseDifficulty(levelArg)
	if !valid {
		return true, fmt.Sprintf("⚠️ Level *%s* tidak dikenal.\nPilih: *easy*, *normal*, atau *hard*.\nContoh: !level easy", strings.TrimSpace(levelArg)), false
	}
	session.Difficulty = difficulty
	return true, fmt.Sprintf("✅ Level diubah ke *%s* (%s).\nHost ketik !mulai kalau semua sudah siap.", difficulty, difficulty.Description()), false
}

func (m *Manager) showLeaderboard(chatJID string, prefixed bool) (bool, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session := m.sessions[chatJID]; session != nil {
		if session.State == stateJoining {
			return true, session.participantListText(), false
		}
		return true, session.leaderboardText(), false
	}
	if board, exists := m.lastBoards[chatJID]; exists {
		return true, board, false
	}
	if !prefixed {
		return false, "", false
	}
	return true, "📭 Belum ada leaderboard di chat ini. Ketik !typing untuk mulai.", false
}

func (m *Manager) submitAnswer(chatJID, userID, text string) (bool, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := m.sessions[chatJID]
	if session == nil || session.State != stateRunning {
		return false, "", false
	}
	participant := session.Participants[userID]
	if participant == nil || participant.Finished {
		return false, "", false
	}

	elapsed := m.now().Sub(session.StartedAt)
	if elapsed < time.Second {
		elapsed = time.Second
	}

	target := normalizeAnswer(session.Sentence)
	typed := normalizeAnswer(text)
	accuracy := 1.0
	if len(target) > 0 {
		distance := levenshtein([]rune(target), []rune(typed))
		accuracy = 1 - float64(distance)/float64(len([]rune(target)))
		if accuracy < 0 {
			accuracy = 0
		}
	}
	wpm := (float64(len([]rune(typed))) / 5.0) / elapsed.Minutes() * accuracy

	participant.Finished = true
	participant.Elapsed = elapsed
	participant.Accuracy = accuracy
	participant.WPM = wpm

	finished, total := session.progress()
	reply := resultText(participant)
	if finished >= total {
		if session.runTimer != nil {
			session.runTimer.Stop()
		}
		board := session.leaderboardText()
		m.lastBoards[chatJID] = board
		delete(m.sessions, chatJID)
		reply += "\n\n" + board
	} else {
		reply += fmt.Sprintf("\n\n📈 Progres: %d/%d peserta sudah selesai.", finished, total)
	}
	return true, reply, false
}

func (m *Manager) expireJoin(chatJID string, session *Session) {
	m.mu.Lock()
	if m.sessions[chatJID] != session || session.State != stateJoining {
		m.mu.Unlock()
		return
	}
	delete(m.sessions, chatJID)
	m.mu.Unlock()

	m.send(chatJID, "⌛ Waktu join habis. Sesi tes mengetik dibatalkan.\nKetik !typing untuk memulai lagi.")
}

func (m *Manager) expireRun(chatJID string, session *Session) {
	m.mu.Lock()
	if m.sessions[chatJID] != session || session.State != stateRunning {
		m.mu.Unlock()
		return
	}
	board := session.leaderboardText()
	m.lastBoards[chatJID] = board
	delete(m.sessions, chatJID)
	m.mu.Unlock()

	m.send(chatJID, "⏰ *Waktu habis!*\n\n"+board)
}

func (m *Manager) send(chatJID, text string) {
	m.mu.Lock()
	sender := m.sender
	m.mu.Unlock()
	if sender != nil {
		sender(chatJID, text)
	}
}

func (s *Session) addParticipant(userID, userName string) {
	if _, exists := s.Participants[userID]; exists {
		return
	}
	s.Participants[userID] = &Participant{
		UserID: userID,
		Name:   displayName(userName, userID),
	}
	s.Order = append(s.Order, userID)
}

func (s *Session) progress() (int, int) {
	finished := 0
	for _, participant := range s.Participants {
		if participant.Finished {
			finished++
		}
	}
	return finished, len(s.Participants)
}

func (s *Session) participantListText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "📋 *Peserta Tes Mengetik — %s*\n\n", s.Difficulty)
	for i, id := range s.Order {
		participant := s.Participants[id]
		if participant == nil {
			continue
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, participant.Name)
	}
	host := s.Participants[s.HostID]
	hostName := "-"
	if host != nil {
		hostName = host.Name
	}
	fmt.Fprintf(&b, "\n📊 %d/%d peserta\n👑 Host: %s\n\nHost ketik !mulai untuk memulai.", len(s.Participants), maxParticipants, hostName)
	return b.String()
}

func (s *Session) leaderboardText() string {
	finished := make([]*Participant, 0, len(s.Participants))
	unfinished := make([]*Participant, 0)
	for _, id := range s.Order {
		participant := s.Participants[id]
		if participant == nil {
			continue
		}
		if participant.Finished {
			finished = append(finished, participant)
		} else {
			unfinished = append(unfinished, participant)
		}
	}
	sort.SliceStable(finished, func(i, j int) bool {
		if finished[i].WPM != finished[j].WPM {
			return finished[i].WPM > finished[j].WPM
		}
		if finished[i].Accuracy != finished[j].Accuracy {
			return finished[i].Accuracy > finished[j].Accuracy
		}
		return finished[i].Elapsed < finished[j].Elapsed
	})

	var b strings.Builder
	fmt.Fprintf(&b, "🏁 *Hasil Tes Mengetik — %s*\n\n", s.Difficulty)
	if len(finished) == 0 {
		b.WriteString("⏳ Belum ada yang selesai.\n")
	}
	for i, participant := range finished {
		line := fmt.Sprintf("%s — %.1f WPM | %.1f%% | %.1fs\n", participant.Name, participant.WPM, participant.Accuracy*100, participant.Elapsed.Seconds())
		switch i {
		case 0:
			b.WriteString("🥇 " + line)
		case 1:
			b.WriteString("🥈 " + line)
		case 2:
			b.WriteString("🥉 " + line)
		default:
			fmt.Fprintf(&b, "%d. %s", i+1, line)
		}
	}
	if len(unfinished) > 0 {
		fmt.Fprintf(&b, "\n⌛ Belum selesai: %d peserta\n", len(unfinished))
	}
	fmt.Fprintf(&b, "📊 Peserta: %d", len(s.Participants))
	return b.String()
}

func displayName(name, userID string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = userID
	}
	runes := []rune(name)
	if len(runes) > 30 {
		runes = runes[:30]
	}
	return string(runes)
}

func resultText(participant *Participant) string {
	return fmt.Sprintf("✅ *Selesai!*\n⏱️ Waktu: %.1f detik\n⚡ Kecepatan: %.1f WPM\n🎯 Akurasi: %.1f%%",
		participant.Elapsed.Seconds(), participant.WPM, participant.Accuracy*100)
}

func normalizeAnswer(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(text))), " ")
}

func levenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min3(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
