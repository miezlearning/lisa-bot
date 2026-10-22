package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsapp-bot/internal/modules/sticker"
)

var (
	stickerExactPattern = regexp.MustCompile(
		`(?i)^\s*(?:@\S+\s+)*(?:(?:ai\s+)?lisa\s+)?[.!\/#]?\s*(?:(?:buat|bikin|bikinin|jadikan|jadiin)\s+)?(?:sticker|stiker|stikker)(?:in|kan)?\s*$`,
	)
	stickerShortPattern = regexp.MustCompile(
		`(?i)^(?:sticker|stiker|stikker)(?:\s+(?:dong|ya|yah|kak|please|pls|min|bang|gan|sih|deh))?$`,
	)
	stickerMentionPattern = regexp.MustCompile(`(?i)@\S+`)
	stickerBotNamePattern = regexp.MustCompile(`(?i)\b(?:ai\s+)?lisa\b`)
	stickerWordPattern    = regexp.MustCompile(`(?i)\b(?:sticker|stiker|stikker)(?:in|kan)?\b`)
	stickerVerbPattern    = regexp.MustCompile(
		`(?i)\b(?:buat|buatkan|bikin|bikinin|bikinkan|jadikan|jadiin|jadi|ubah|rubah|ubahin|konversi|convert|stikerin|stikerkan)\b`,
	)
	stickerQuestionPattern = regexp.MustCompile(
		`(?i)\b(?:gimana|bagaimana|gmn|apa|apakah|cara|how|what|kenapa|kok|jelaskan|jelasin|pengertian)\b`,
	)
)

type stickerSource struct {
	downloadable whatsmeow.DownloadableMessage
	ext          string
	animated     bool
	fileLength   uint64
}

// isStickerCommand reports whether text asks to create a sticker.
func isStickerCommand(text string) bool {
	if stickerExactPattern.MatchString(text) {
		return true
	}

	normalized := normalizeStickerText(text)
	if normalized == "" {
		return false
	}
	if stickerQuestionPattern.MatchString(normalized) {
		return false
	}
	if stickerShortPattern.MatchString(normalized) {
		return true
	}
	return stickerWordPattern.MatchString(normalized) && stickerVerbPattern.MatchString(normalized)
}

func normalizeStickerText(text string) string {
	normalized := strings.ToLower(strings.TrimSpace(text))
	normalized = stickerMentionPattern.ReplaceAllString(normalized, " ")
	normalized = stickerBotNamePattern.ReplaceAllString(normalized, " ")
	normalized = strings.TrimSpace(normalized)
	normalized = strings.TrimLeft(normalized, ".!/# ")
	return strings.Join(strings.Fields(normalized), " ")
}

// extractMediaCaption returns the caption of an attached image/video/document.
func extractMediaCaption(msg *waE2E.Message) string {
	msg = unwrapMessage(msg)
	if msg == nil {
		return ""
	}
	switch {
	case msg.ImageMessage != nil:
		return msg.ImageMessage.GetCaption()
	case msg.VideoMessage != nil:
		return msg.VideoMessage.GetCaption()
	case msg.DocumentMessage != nil:
		return msg.DocumentMessage.GetCaption()
	}
	return ""
}

// findStickerMedia picks a supported media attachment from a message.
func findStickerMedia(msg *waE2E.Message) *stickerSource {
	msg = unwrapMessage(msg)
	if msg == nil {
		return nil
	}

	switch {
	case msg.ImageMessage != nil:
		return &stickerSource{
			downloadable: msg.ImageMessage,
			ext:          imageExtension(msg.ImageMessage.GetMimetype()),
			fileLength:   msg.ImageMessage.GetFileLength(),
		}
	case msg.StickerMessage != nil:
		return &stickerSource{
			downloadable: msg.StickerMessage,
			ext:          ".webp",
			animated:     msg.StickerMessage.GetIsAnimated(),
			fileLength:   msg.StickerMessage.GetFileLength(),
		}
	case msg.VideoMessage != nil:
		return &stickerSource{
			downloadable: msg.VideoMessage,
			ext:          videoExtension(msg.VideoMessage.GetMimetype()),
			animated:     true,
			fileLength:   msg.VideoMessage.GetFileLength(),
		}
	case msg.DocumentMessage != nil:
		ext, ok := extensionForMime(msg.DocumentMessage.GetMimetype())
		if !ok {
			return nil
		}
		return &stickerSource{
			downloadable: msg.DocumentMessage,
			ext:          ext,
			animated:     isAnimatedExtension(ext),
			fileLength:   msg.DocumentMessage.GetFileLength(),
		}
	}
	return nil
}

func imageExtension(mime string) string {
	if ext, ok := extensionForMime(mime); ok {
		return ext
	}
	return ".jpg"
}

func videoExtension(mime string) string {
	if ext, ok := extensionForMime(mime); ok {
		return ext
	}
	return ".mp4"
}

func extensionForMime(mime string) (string, bool) {
	mime = strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0]))
	switch mime {
	case "image/jpeg", "image/jpg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	case "video/mp4":
		return ".mp4", true
	case "video/webm":
		return ".webm", true
	case "video/quicktime":
		return ".mov", true
	}
	return "", false
}

func isAnimatedExtension(ext string) bool {
	switch ext {
	case ".gif", ".mp4", ".webm", ".mov":
		return true
	}
	return false
}

// handleSticker resolves the media, then converts and sends it asynchronously.
func (h *EventHandler) handleSticker(evt *events.Message) {
	media := findStickerMedia(evt.Message)
	if media == nil {
		if ci := getContextInfo(evt.Message); ci != nil {
			media = findStickerMedia(ci.GetQuotedMessage())
		}
	}

	if media == nil {
		h.sendReply(evt, "🖼️ *Cara membuat stiker:*\n\n"+
			"1. Kirim gambar/GIF dengan caption `!stiker`\n"+
			"2. Atau reply gambar/GIF lalu ketik `!stiker`\n\n"+
			"_Bisa juga pakai `.stiker`, `!sticker`, atau `buat stiker`._")
		return
	}

	if media.fileLength > sticker.MaxInputBytes {
		h.sendReply(evt, fmt.Sprintf("❌ Media terlalu besar. Maksimal %d MB.", sticker.MaxInputBytes/(1024*1024)))
		return
	}

	ctx := context.Background()
	if !evt.Info.IsFromMe {
		_ = h.client.MarkRead(ctx, []types.MessageID{evt.Info.ID}, time.Now(), evt.Info.Chat, evt.Info.Sender)
	}

	fmt.Printf("🖼️ [Stiker] Memproses media dari %s (animated: %v)\n", evt.Info.Sender.String(), media.animated)
	go h.processSticker(evt, media)
}

func (h *EventHandler) processSticker(evt *events.Message, media *stickerSource) {
	select {
	case h.stickerSem <- struct{}{}:
		defer func() { <-h.stickerSem }()
	case <-time.After(30 * time.Second):
		h.sendReply(evt, "⏳ Antrean pembuatan stiker sedang penuh. Coba lagi sebentar ya.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), sticker.DefaultTimeout)
	defer cancel()

	data, err := h.client.Download(ctx, media.downloadable)
	if err != nil {
		fmt.Printf("❌ [Stiker] Gagal unduh media: %v\n", err)
		h.sendReply(evt, "❌ Gagal mengunduh media. Coba kirim ulang atau perkecil ukurannya.")
		return
	}
	if len(data) > sticker.MaxInputBytes {
		h.sendReply(evt, fmt.Sprintf("❌ Media terlalu besar. Maksimal %d MB.", sticker.MaxInputBytes/(1024*1024)))
		return
	}

	result, err := sticker.Convert(ctx, data, media.ext, media.animated)
	if err != nil {
		switch {
		case errors.Is(err, sticker.ErrFFmpegNotFound):
			fmt.Printf("❌ [Stiker] ffmpeg tidak ditemukan\n")
			h.sendReply(evt, "⚠️ Fitur stiker belum siap: `ffmpeg` tidak terpasang di server.")
		case errors.Is(err, sticker.ErrInputTooLarge), errors.Is(err, sticker.ErrTooLarge):
			fmt.Printf("❌ [Stiker] Ukuran melebihi batas: %v\n", err)
			h.sendReply(evt, "❌ Stiker terlalu besar untuk WhatsApp. Coba gambar/GIF yang lebih kecil atau lebih pendek.")
		default:
			fmt.Printf("❌ [Stiker] Gagal konversi: %v\n", err)
			h.sendReply(evt, "❌ Gagal membuat stiker. Pastikan media berupa gambar, GIF, atau video pendek.")
		}
		return
	}

	if err := h.sendSticker(ctx, evt, result); err != nil {
		fmt.Printf("❌ [Stiker] Gagal kirim stiker: %v\n", err)
		h.sendReply(evt, "❌ Stiker berhasil dibuat, tapi gagal dikirim. Coba lagi ya.")
	}
}

func (h *EventHandler) sendSticker(ctx context.Context, evt *events.Message, result *sticker.Result) error {
	upload, err := h.client.Upload(ctx, result.Data, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	msg := &waE2E.StickerMessage{
		URL:           proto.String(upload.URL),
		DirectPath:    proto.String(upload.DirectPath),
		MediaKey:      upload.MediaKey,
		FileEncSHA256: upload.FileEncSHA256,
		FileSHA256:    upload.FileSHA256,
		FileLength:    proto.Uint64(upload.FileLength),
		Mimetype:      proto.String("image/webp"),
		Width:         proto.Uint32(sticker.Size),
		Height:        proto.Uint32(sticker.Size),
		IsAnimated:    proto.Bool(result.Animated),
		StickerSentTS: proto.Int64(time.Now().UnixMilli()),
	}

	if evt.Info.IsGroup {
		stanzaID := string(evt.Info.ID)
		participant := evt.Info.Sender.ToNonAD().String()
		msg.ContextInfo = &waE2E.ContextInfo{
			StanzaID:      &stanzaID,
			Participant:   &participant,
			QuotedMessage: evt.Message,
		}
	}

	if _, err := h.client.SendMessage(ctx, evt.Info.Chat, &waE2E.Message{StickerMessage: msg}); err != nil {
		return err
	}

	h.mu.Lock()
	h.lastReply[evt.Info.Chat.String()] = time.Now()
	h.mu.Unlock()

	fmt.Printf("📤 [Stiker Terkirim] Ke: %s (Animated: %v | Ukuran: %d KB)\n",
		evt.Info.Chat.String(), result.Animated, len(result.Data)/1024)
	return nil
}
