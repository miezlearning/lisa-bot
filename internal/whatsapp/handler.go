package whatsapp

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-bot/internal/database"
	"whatsapp-bot/internal/nlu"
	"whatsapp-bot/internal/router"
)

// EventHandler registers message listeners and dispatches messages through NLU and Router.
type EventHandler struct {
	client *whatsmeow.Client
	db     *database.DB
	engine *nlu.Engine
	router *router.Router
}

// NewEventHandler creates an EventHandler instance.
func NewEventHandler(client *whatsmeow.Client, db *database.DB, engine *nlu.Engine, r *router.Router) *EventHandler {
	return &EventHandler{
		client: client,
		db:     db,
		engine: engine,
		router: r,
	}
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

	// 3. Group Whitelist & Mention/Reply Check
	if evt.Info.IsGroup {
		chatJIDStr := evt.Info.Chat.String()
		senderPhone := evt.Info.Sender.User
		lowerRaw := strings.ToLower(rawText)

		// Check registered admins count
		adminCount, _ := h.db.GetAdminCount(ctx)

		// Bootstrap: Jika belum ada admin terdaftar di database sama sekali
		if adminCount == 0 {
			if strings.Contains(lowerRaw, "izinkan grup") || strings.Contains(lowerRaw, "klaim admin") {
				_ = h.db.AddAdmin(ctx, senderPhone, evt.Info.PushName, "bootstrap")
				fmt.Printf("👑 [Bootstrap] Admin pertama otomatis didaftarkan: %s (%s)\n", senderPhone, evt.Info.PushName)
			}
		}

		// Check if group is authorized in database
		isAllowed := h.db.IsGroupAllowed(ctx, chatJIDStr)
		if !isAllowed {
			isAdmin := h.db.IsAdmin(ctx, senderPhone)
			isAllowCmd := strings.Contains(lowerRaw, "izinkan grup") || strings.Contains(lowerRaw, "allow group")

			if isAdmin && isAllowCmd {
				fmt.Printf("🔓 [Whitelist] Memproses perintah izin grup dari admin: %s\n", senderPhone)
			} else {
				// Jika grup belum diizinkan dan bot belum punya admin sama sekali, beri petunjuk
				if adminCount == 0 && (strings.Contains(lowerRaw, "@bot") || strings.HasPrefix(lowerRaw, "!")) {
					fmt.Printf("⚠️ Grup %s belum diizinkan dan belum ada admin terdaftar.\n", chatJIDStr)
					h.sendReply(evt, "⚠️ *Bot Belum Diizinkan di Grup Ini*\n\nBelum ada admin yang didaftarkan di sistem. Untuk mengizinkan grup ini dan mendaftarkan kamu sebagai admin pertama, ketik:\n`@Bot izinkan grup`")
					return
				}

				fmt.Printf("🚫 [Abaikan] Grup %s belum diizinkan dalam database whitelist.\n", chatJIDStr)
				return
			}
		}

		// Check if bot was mentioned or replied to in the group
		if !h.shouldRespondInGroup(evt, rawText) {
			fmt.Printf("ℹ️ [Abaikan] Bot tidak di-mention atau di-reply di grup %s.\n", chatJIDStr)
			return
		}
	}

	// 4. Strip mentions (@628xxx, @bot, etc.) before NLU processing
	cleanText := nlu.CleanMentions(rawText)
	if strings.TrimSpace(cleanText) == "" {
		cleanText = "help"
	}

	// 5. Process through Deterministic NLU Engine
	nluResult, err := h.engine.Process(ctx, cleanText)
	if err != nil {
		fmt.Printf("❌ NLU processing error: %v\n", err)
		return
	}

	// 6. Build Message Context
	msgCtx := &router.MessageContext{
		Context:      ctx,
		SenderJID:    evt.Info.Sender,
		SenderName:   evt.Info.PushName,
		ChatJID:      evt.Info.Chat,
		IsGroup:      evt.Info.IsGroup,
		RawMessage:   rawText,
		CleanMessage: cleanText,
		NLUResult:    nluResult,
	}

	// 7. Route and Execute
	replyText, err := h.router.Route(msgCtx)
	if err != nil {
		replyText = fmt.Sprintf("⚠️ Terjadi kesalahan: %v", err)
	}

	// 8. Auto-Correct Transparency Header: HANYA DITAMPILKAN DI GROUP CHAT
	var autoCorrectHeader string
	if evt.Info.IsGroup {
		autoCorrectHeader = router.FormatAutoCorrectHeader(nluResult.Corrections)
	}
	finalResponse := autoCorrectHeader + replyText

	// 9. Send WhatsApp Response
	h.sendReply(evt, finalResponse)
}

func (h *EventHandler) shouldRespondInGroup(evt *events.Message, rawText string) bool {
	lower := strings.ToLower(strings.TrimSpace(rawText))

	// Prefix @bot, !, or /
	if strings.Contains(lower, "@bot") || strings.HasPrefix(lower, "!") || strings.HasPrefix(lower, "/") {
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

	// Check if text directly contains bot phone number e.g. @6285928105840
	if botPhoneJID.User != "" && strings.Contains(lower, "@"+botPhoneJID.User) {
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
		// In private chat, Participant must NOT be set
		respMsg = &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: &text,
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:      &stanzaID,
					QuotedMessage: evt.Message,
				},
			},
		}
	}

	_, err := h.client.SendMessage(context.Background(), evt.Info.Chat, respMsg)
	if err != nil {
		fmt.Printf("⚠️ Gagal kirim pesan quoted: %v, mencoba pesan unquoted...\n", err)
		_, errFallback := h.client.SendMessage(context.Background(), evt.Info.Chat, &waE2E.Message{Conversation: &text})
		if errFallback != nil {
			fmt.Printf("❌ Fallback kirim pesan juga gagal: %v\n", errFallback)
			return
		}
	}

	fmt.Printf("📤 [Balasan Terkirim] Ke: %s\n", evt.Info.Chat.String())
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
