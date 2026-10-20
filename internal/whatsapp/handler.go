package whatsapp

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bot/internal/database"
	"whatsapp-bot/internal/modules/typing"
	"whatsapp-bot/internal/nlu"
	"whatsapp-bot/internal/router"
)

const (
	historyLimit = 12
	historyTTL   = 30 * time.Minute
)

// EventHandler registers message listeners and dispatches messages through NLU and Router.
type EventHandler struct {
	client    *whatsmeow.Client
	db        *database.DB
	engine    *nlu.Engine
	router    *router.Router
	typing    *typing.Manager
	lastReply map[string]time.Time
	history   map[string][]nlu.ChatMessage
	historyAt map[string]time.Time
	mu        sync.Mutex
	random    *rand.Rand
}

// NewEventHandler creates an EventHandler instance.
func NewEventHandler(client *whatsmeow.Client, db *database.DB, engine *nlu.Engine, r *router.Router, typingManager *typing.Manager) *EventHandler {
	if typingManager == nil {
		typingManager = typing.NewManager()
	}
	return &EventHandler{
		client:    client,
		db:        db,
		engine:    engine,
		router:    r,
		typing:    typingManager,
		lastReply: make(map[string]time.Time),
		history:   make(map[string][]nlu.ChatMessage),
		historyAt: make(map[string]time.Time),
		random:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (h *EventHandler) getHistory(key string) []nlu.ChatMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

	turns, ok := h.history[key]
	if !ok {
		return nil
	}
	if time.Since(h.historyAt[key]) > historyTTL {
		delete(h.history, key)
		delete(h.historyAt, key)
		return nil
	}
	out := make([]nlu.ChatMessage, len(turns))
	copy(out, turns)
	return out
}

func (h *EventHandler) appendHistory(key, userText, assistantText string) {
	if strings.TrimSpace(userText) == "" || strings.TrimSpace(assistantText) == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	turns := append(h.history[key],
		nlu.ChatMessage{Role: "user", Text: userText},
		nlu.ChatMessage{Role: "assistant", Text: assistantText},
	)
	if len(turns) > historyLimit {
		turns = turns[len(turns)-historyLimit:]
	}
	h.history[key] = turns
	h.historyAt[key] = time.Now()
}

// Register registers the event listener on the whatsmeow client.
func (h *EventHandler) Register() {
	h.client.AddEventHandler(h.handleEvent)
}

func (h *EventHandler) handleEvent(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.Message:
		h.handleMessage(evt)
	}
}

func (h *EventHandler) handleMessage(evt *events.Message) {
	// 1. Ignore messages from self or empty messages
	if evt.Info.IsFromMe || evt.Message == nil {
		return
	}

	// 2. Extract message text (supports normal, ephemeral/disappearing, view once, and caption messages)
	rawText := extractMessageText(evt.Message)
	if strings.TrimSpace(rawText) == "" {
		return
	}

	fmt.Printf("\n📩 [Pesan Masuk] Dari: %s | Chat: %s (Grup: %v)\n   Teks: %q\n",
		evt.Info.Sender.String(), evt.Info.Chat.String(), evt.Info.IsGroup, rawText)

	ctx := context.Background()
	senderPhone, senderLID := h.resolveSender(ctx, evt)
	chatJIDStr := evt.Info.Chat.String()
	userID := evt.Info.Sender.ToNonAD().String()

	fmt.Printf("\n📩 [Pesan Masuk] Dari: %s (Phone: %s, LID: %s) | Chat: %s (Grup: %v)\n   Teks: %q\n",
		evt.Info.Sender.String(), senderPhone, senderLID, evt.Info.Chat.String(), evt.Info.IsGroup, rawText)

	// 3. Group Whitelist & Mention/Reply Check
	if evt.Info.IsGroup {
		lowerRaw := strings.ToLower(rawText)

		// Check registered admins count
		adminCount, _ := h.db.GetAdminCount(ctx)

		// Check if sender is registered admin (by phone or LID)
		isAdmin := (senderPhone != "" && h.db.IsAdmin(ctx, senderPhone)) || (senderLID != "" && h.db.IsAdmin(ctx, senderLID))

		// If admin verified by phone, auto-link their LID as admin alias
		if isAdmin && senderLID != "" && !h.db.IsAdmin(ctx, senderLID) {
			_ = h.db.AddAdmin(ctx, senderLID, evt.Info.PushName, "lid_alias:"+senderPhone)
			fmt.Printf("🔗 [LID Linked] Akun LID %s otomatis ditautkan sebagai admin alias dari %s\n", senderLID, senderPhone)
		}

		// Bootstrap: Jika belum ada admin terdaftar di database sama sekali
		if adminCount == 0 {
			if strings.Contains(lowerRaw, "izinkan grup") || strings.Contains(lowerRaw, "klaim admin") {
				adminID := senderPhone
				if adminID == "" {
					adminID = senderLID
				}
				_ = h.db.AddAdmin(ctx, adminID, evt.Info.PushName, "bootstrap")
				if senderLID != "" && senderLID != adminID {
					_ = h.db.AddAdmin(ctx, senderLID, evt.Info.PushName, "bootstrap_lid")
				}
				isAdmin = true
				fmt.Printf("👑 [Bootstrap] Admin pertama otomatis didaftarkan: %s (%s)\n", adminID, evt.Info.PushName)
			}
		}

		// Check if group is authorized in database
		isAllowed := h.db.IsGroupAllowed(ctx, chatJIDStr)
		if !isAllowed {
			isAllowCmd := strings.Contains(lowerRaw, "izinkan grup") || strings.Contains(lowerRaw, "allow group")

			if isAdmin && isAllowCmd {
				fmt.Printf("🔓 [Whitelist] Memproses perintah izin grup dari admin: Phone=%s, LID=%s\n", senderPhone, senderLID)
			} else {
				if adminCount == 0 && (strings.Contains(lowerRaw, "@bot") || strings.Contains(lowerRaw, "@lisa") || strings.HasPrefix(lowerRaw, "!")) {
					fmt.Printf("⚠️ Grup %s belum diizinkan dan belum ada admin terdaftar.\n", chatJIDStr)
					h.sendReply(evt, "⚠️ *Bot Belum Diizinkan di Grup Ini*\n\nBelum ada admin yang didaftarkan di sistem. Untuk mengizinkan grup ini dan mendaftarkan kamu sebagai admin pertama, ketik:\n`@Bot izinkan grup`")
					return
				}

				fmt.Printf("🚫 [Abaikan] Grup %s belum diizinkan dalam database whitelist.\n", chatJIDStr)
				fmt.Printf("   💡 Petunjuk: Admin dapat mengizinkan grup ini dengan mengirim pesan japri ke bot:\n   izinkan grup %s\n", chatJIDStr)
				return
			}
		}

		// Check if bot was mentioned or replied to in the group.
		// Active typing participants and session-aware bare commands bypass this check.
		if !h.shouldRespondInGroup(evt, rawText) && !h.typing.NeedsHandling(chatJIDStr, userID, rawText) {
			fmt.Printf("ℹ️ [Abaikan] Bot tidak di-mention atau di-reply di grup %s.\n", chatJIDStr)
			return
		}
	}

	// 4. Typing test commands and answer submissions
	if handled, reply, markStarted := h.typing.HandleMessage(chatJIDStr, userID, evt.Info.PushName, rawText); handled {
		if reply != "" {
			h.sendReply(evt, reply)
		}
		if markStarted {
			h.typing.MarkStarted(chatJIDStr)
		}
		return
	}

	// 5. Strip mentions (@628xxx, @bot, etc.) before NLU processing
	cleanText := nlu.CleanMentions(rawText)
	if strings.TrimSpace(cleanText) == "" {
		cleanText = "help"
	}

	// 6. Process through Deterministic NLU Engine
	nluResult, err := h.engine.Process(ctx, cleanText)
	if err != nil {
		fmt.Printf("❌ NLU processing error: %v\n", err)
		return
	}

	historyKey := evt.Info.Chat.String() + "|" + evt.Info.Sender.String()
	history := h.getHistory(historyKey)

	// 7. Build Message Context
	msgCtx := &router.MessageContext{
		Context:      ctx,
		SenderJID:    evt.Info.Sender,
		SenderPhone:  senderPhone,
		SenderName:   evt.Info.PushName,
		ChatJID:      evt.Info.Chat,
		IsGroup:      evt.Info.IsGroup,
		RawMessage:   rawText,
		CleanMessage: cleanText,
		History:      history,
		NLUResult:    nluResult,
	}

	// 8. Route and Execute
	replyText, err := h.router.Route(msgCtx)
	if err != nil {
		replyText = fmt.Sprintf("⚠️ Terjadi kesalahan: %v", err)
	}

	// 9. Auto-Correct: Dijalankan senyap (silent) agar balasan bot bersih & natural
	if len(nluResult.Corrections) > 0 {
		fmt.Printf("✏️ [Auto-Correct] %s\n", strings.Join(nluResult.Corrections, ", "))
	}
	finalResponse := replyText

	// 10. Simpan riwayat hanya untuk percakapan natural (bukan command/query terstruktur)
	if nluResult.Intent == nlu.IntentConversational {
		h.appendHistory(historyKey, cleanText, finalResponse)
	}

	// 11. Send WhatsApp Response
	h.sendReply(evt, finalResponse)
}

// resolveSender resolves the sender's phone number and LID.
func (h *EventHandler) resolveSender(ctx context.Context, evt *events.Message) (string, string) {
	var phone, lid string

	if evt.Info.Sender.Server == "lid" {
		lid = evt.Info.Sender.User
	} else if evt.Info.Sender.Server == types.DefaultUserServer || evt.Info.Sender.Server == "" {
		phone = evt.Info.Sender.User
	}

	if evt.Info.SenderAlt.Server == types.DefaultUserServer && evt.Info.SenderAlt.User != "" {
		phone = evt.Info.SenderAlt.User
	} else if evt.Info.SenderAlt.Server == "lid" && evt.Info.SenderAlt.User != "" {
		lid = evt.Info.SenderAlt.User
	}

	// Try whatsmeow's internal LID mapping store
	if phone == "" && lid != "" && h.client != nil && h.client.Store != nil && h.client.Store.LIDs != nil {
		pn, err := h.client.Store.LIDs.GetPNForLID(ctx, evt.Info.Sender.ToNonAD())
		if err == nil && !pn.IsEmpty() && pn.User != "" {
			phone = pn.User
		}
	}

	// Try querying group participant list
	if phone == "" && evt.Info.IsGroup && h.client != nil {
		groupInfo, err := h.client.GetGroupInfo(ctx, evt.Info.Chat)
		if err == nil && groupInfo != nil {
			for _, p := range groupInfo.Participants {
				pLID := p.LID.ToNonAD().User
				pJID := p.JID.ToNonAD().User
				pPN := p.PhoneNumber.ToNonAD().User

				if lid != "" && (pLID == lid || pJID == lid) {
					if pPN != "" {
						phone = pPN
						break
					}
					if p.JID.Server == types.DefaultUserServer && pJID != "" {
						phone = pJID
						break
					}
				}
			}
		}
	}

	return phone, lid
}

func (h *EventHandler) shouldRespondInGroup(evt *events.Message, rawText string) bool {
	lower := strings.ToLower(strings.TrimSpace(rawText))

	// Prefix @bot, @lisa, !, /, ., or #
	if strings.Contains(lower, "@bot") || strings.Contains(lower, "@lisa") ||
		strings.HasPrefix(lower, "!") || strings.HasPrefix(lower, "/") ||
		strings.HasPrefix(lower, ".") || strings.HasPrefix(lower, "#") {
		return true
	}

	var botPhoneJID types.JID
	if h.client.Store.ID != nil {
		botPhoneJID = h.client.Store.ID.ToNonAD()
	}

	var botLID types.JID
	if !h.client.Store.LID.IsEmpty() {
		botLID = h.client.Store.LID.ToNonAD()
	}

	// Check if text directly contains bot phone number or LID e.g. @6285928105840 or @69458949357693
	if botPhoneJID.User != "" && strings.Contains(lower, "@"+botPhoneJID.User) {
		return true
	}
	if botLID.User != "" && strings.Contains(lower, "@"+botLID.User) {
		return true
	}

	contextInfo := getContextInfo(evt.Message)
	if contextInfo == nil {
		return false
	}

	// Condition 1: Bot is mentioned in MentionedJID slice
	mentioned := contextInfo.GetMentionedJID()
	for _, jidStr := range mentioned {
		if (botPhoneJID.User != "" && strings.Contains(jidStr, botPhoneJID.User)) ||
			(botLID.User != "" && strings.Contains(jidStr, botLID.User)) {
			return true
		}
	}

	// Condition 2: Message replies to bot's message
	participant := contextInfo.GetParticipant()
	if participant != "" {
		if (botPhoneJID.User != "" && strings.Contains(participant, botPhoneJID.User)) ||
			(botLID.User != "" && strings.Contains(participant, botLID.User)) {
			return true
		}
	}

	return false
}

func (h *EventHandler) sendReply(evt *events.Message, text string) {
	ctx := context.Background()
	chatJID := evt.Info.Chat

	// 1. Anti-Flood & Rate Limiting per Chat (hindari spam reply beruntun < 1.2 detik)
	h.mu.Lock()
	lastTime, exists := h.lastReply[chatJID.String()]
	h.mu.Unlock()

	if exists {
		elapsed := time.Since(lastTime)
		if elapsed < 1200*time.Millisecond {
			time.Sleep(1200*time.Millisecond - elapsed)
		}
	}

	// 2. Simulasi Waktu Baca Manusia (Realistic Reading Latency: 400ms - 900ms)
	h.mu.Lock()
	readDelay := time.Duration(400+h.random.Intn(500)) * time.Millisecond
	h.mu.Unlock()
	time.Sleep(readDelay)

	// 3. Kirim Tanda Centang Biru (MarkRead / Read Receipt)
	// WhatsApp anti-spam memantau akun yang tidak pernah mengirim status baca
	if !evt.Info.IsFromMe {
		_ = h.client.MarkRead(ctx, []types.MessageID{evt.Info.ID}, time.Now(), chatJID, evt.Info.Sender)
	}

	// 4. Kirim Indikator "Sedang Mengetik..." (Chat Presence Composing)
	_ = h.client.SendChatPresence(ctx, chatJID, types.ChatPresenceComposing, types.ChatPresenceMediaText)

	// 5. Durasi Mengetik Alami Sesuai Panjang Pesan (Typing Speed Simulation)
	// Rata-rata manusia mengetik ~20-30ms per karakter.
	// Diberi batas aman: Minimal 1.2 detik, Maksimal 3.5 detik + variasi random (jitter).
	textLen := len([]rune(text))
	baseTypingMs := textLen * 22
	if baseTypingMs < 1200 {
		baseTypingMs = 1200
	} else if baseTypingMs > 3500 {
		baseTypingMs = 3500
	}

	h.mu.Lock()
	jitterMs := h.random.Intn(400) - 200 // -200ms s/d +200ms
	h.mu.Unlock()

	typingDuration := time.Duration(baseTypingMs+jitterMs) * time.Millisecond
	if typingDuration < 1000*time.Millisecond {
		typingDuration = 1000 * time.Millisecond
	}

	time.Sleep(typingDuration)

	// 6. Matikan status mengetik sebelum mengirim (Chat Presence Paused)
	_ = h.client.SendChatPresence(ctx, chatJID, types.ChatPresencePaused, types.ChatPresenceMediaText)

	// 7. Kirim Pesan dengan Format Quoted / Unquoted Fallback
	stanzaID := evt.Info.ID
	var respMsg *waE2E.Message

	if evt.Info.IsGroup {
		participant := evt.Info.Sender.ToNonAD().String()
		respMsg = &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: &text,
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:      &stanzaID,
					Participant:   &participant,
					QuotedMessage: evt.Message,
				},
			},
		}
	} else {
		// In private chat, send a plain message without quoting the user's text.
		// Quoting in DMs makes every reply look like an echo of the incoming message.
		respMsg = &waE2E.Message{
			Conversation: &text,
		}
	}

	_, err := h.client.SendMessage(ctx, chatJID, respMsg)
	if err != nil {
		fmt.Printf("⚠️ Gagal kirim pesan quoted: %v, mencoba pesan unquoted...\n", err)
		_, errFallback := h.client.SendMessage(ctx, chatJID, &waE2E.Message{Conversation: &text})
		if errFallback != nil {
			fmt.Printf("❌ Fallback kirim pesan juga gagal: %v\n", errFallback)
			return
		}
	}

	// Update timestamp pengiriman terakhir untuk rate limiter chat ini
	h.mu.Lock()
	h.lastReply[chatJID.String()] = time.Now()
	h.mu.Unlock()

	fmt.Printf("📤 [Balasan Terkirim] Ke: %s (Karakter: %d | Waktu Ketik: %v)\n",
		chatJID.String(), textLen, typingDuration)
}

func (h *EventHandler) SendText(chatJID, text string) {
	jid, err := types.ParseJID(chatJID)
	if err != nil {
		fmt.Printf("⚠️ Gagal parse JID %s: %v\n", chatJID, err)
		return
	}
	_, err = h.client.SendMessage(context.Background(), jid, &waE2E.Message{Conversation: &text})
	if err != nil {
		fmt.Printf("❌ Gagal kirim pesan ke %s: %v\n", chatJID, err)
		return
	}
	fmt.Printf("📤 [Pesan Terkirim] Ke: %s (Timeout/Notifikasi)\n", chatJID)
}

func extractMessageText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	// Unwrap ephemeral message (disappearing messages)
	if msg.EphemeralMessage != nil && msg.EphemeralMessage.Message != nil {
		msg = msg.EphemeralMessage.Message
	}
	// Unwrap view once message
	if msg.ViewOnceMessage != nil && msg.ViewOnceMessage.Message != nil {
		msg = msg.ViewOnceMessage.Message
	}
	// Unwrap document with caption
	if msg.DocumentWithCaptionMessage != nil && msg.DocumentWithCaptionMessage.Message != nil {
		msg = msg.DocumentWithCaptionMessage.Message
	}
	if msg.Conversation != nil && *msg.Conversation != "" {
		return *msg.Conversation
	}
	if msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.Text != nil {
		return *msg.ExtendedTextMessage.Text
	}
	return ""
}

func getContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	if msg == nil {
		return nil
	}
	if msg.EphemeralMessage != nil && msg.EphemeralMessage.Message != nil {
		msg = msg.EphemeralMessage.Message
	}
	if msg.ViewOnceMessage != nil && msg.ViewOnceMessage.Message != nil {
		msg = msg.ViewOnceMessage.Message
	}
	if msg.ExtendedTextMessage != nil {
		return msg.ExtendedTextMessage.ContextInfo
	}
	return nil
}
