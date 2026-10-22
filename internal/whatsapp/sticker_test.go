package whatsapp

import (
	"testing"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestIsStickerCommand(t *testing.T) {
	cases := map[string]bool{
		"stiker":                       true,
		"sticker":                      true,
		".stiker":                      true,
		"!sticker":                     true,
		"/stiker":                      true,
		"#sticker":                     true,
		"buat stiker":                  true,
		"bikin stiker":                 true,
		"jadikan sticker":              true,
		"lisa buat stiker":             true,
		"@bot stiker":                  true,
		"@628123456789 stiker":         true,
		"stikerin":                     true,
		"stiker dong":                  true,
		"@bot jadikan stiker":          true,
		"@AI Lisa buatin stiker":       true,
		"tolong buatin stiker dong":    true,
		"tolong jadikan stiker dong":   true,
		"bikin stiker dari gambar ini": true,
		"ubah foto ini jadi stiker":    true,
		"gambar ini jadi stiker":       true,
		"tolong stikerin gambar ini":   true,
		"menu":                         false,
		"cara bikin stiker":            false,
		"gimana cara bikin stiker":     false,
		"apa itu stiker":               false,
		"jelaskan cara buat stiker":    false,
		"kirim stiker dong":            false,
		"stiker nya bagus":             false,
		"":                             false,
	}

	for input, want := range cases {
		if got := isStickerCommand(input); got != want {
			t.Errorf("isStickerCommand(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestFindStickerMedia(t *testing.T) {
	t.Run("image", func(t *testing.T) {
		msg := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			Mimetype:   proto.String("image/jpeg"),
			FileLength: proto.Uint64(2048),
			Caption:    proto.String("!stiker"),
		}}

		media := findStickerMedia(msg)
		if media == nil {
			t.Fatal("media gambar tidak terdeteksi")
		}
		if media.ext != ".jpg" || media.animated {
			t.Fatalf("media salah: ext=%s animated=%v", media.ext, media.animated)
		}
		if media.fileLength != 2048 {
			t.Fatalf("fileLength = %d, want 2048", media.fileLength)
		}
		if got := extractMediaCaption(msg); got != "!stiker" {
			t.Fatalf("caption = %q, want !stiker", got)
		}
	})

	t.Run("gif document", func(t *testing.T) {
		msg := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			Mimetype: proto.String("image/gif"),
		}}

		media := findStickerMedia(msg)
		if media == nil {
			t.Fatal("media GIF tidak terdeteksi")
		}
		if media.ext != ".gif" || !media.animated {
			t.Fatalf("media salah: ext=%s animated=%v", media.ext, media.animated)
		}
	})

	t.Run("video", func(t *testing.T) {
		msg := &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			Mimetype: proto.String("video/mp4"),
		}}

		media := findStickerMedia(msg)
		if media == nil {
			t.Fatal("media video tidak terdeteksi")
		}
		if media.ext != ".mp4" || !media.animated {
			t.Fatalf("media salah: ext=%s animated=%v", media.ext, media.animated)
		}
	})

	t.Run("unsupported document", func(t *testing.T) {
		msg := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			Mimetype: proto.String("application/pdf"),
		}}
		if media := findStickerMedia(msg); media != nil {
			t.Fatalf("dokumen PDF tidak boleh didukung, dapat %+v", media)
		}
	})
}
