package sticker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// Size is the required WhatsApp sticker dimension (square).
	Size = 512
	// MaxStaticBytes is WhatsApp's size limit for static stickers.
	MaxStaticBytes = 100 * 1024
	// MaxAnimatedBytes is WhatsApp's size limit for animated stickers.
	MaxAnimatedBytes = 500 * 1024
	// MaxInputBytes caps the downloaded source media.
	MaxInputBytes = 25 * 1024 * 1024
	// DefaultTimeout bounds a single conversion.
	DefaultTimeout = 90 * time.Second
)

var (
	ErrFFmpegNotFound = errors.New("ffmpeg tidak ditemukan di PATH")
	ErrInputTooLarge  = errors.New("media sumber terlalu besar")
	ErrTooLarge       = errors.New("stiker melebihi batas ukuran WhatsApp")
)

// Result holds an encoded WebP sticker.
type Result struct {
	Data     []byte
	Animated bool
}

// Available reports whether ffmpeg is installed and reachable.
func Available() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// Convert turns an image, GIF, short video, or existing WebP into a
// WhatsApp-compatible 512x512 WebP sticker.
func Convert(ctx context.Context, data []byte, ext string, animatedHint bool) (*Result, error) {
	if len(data) == 0 {
		return nil, errors.New("media kosong")
	}
	if len(data) > MaxInputBytes {
		return nil, fmt.Errorf("%w: maksimal %d MB", ErrInputTooLarge, MaxInputBytes/(1024*1024))
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, ErrFFmpegNotFound
	}

	ext = normalizeExt(ext)
	animated := animatedHint || isAnimatedExt(ext)
	if ext == ".webp" && isAnimatedWebP(data) {
		animated = true
	}

	dir, err := os.MkdirTemp("", "lisa-sticker-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	input := filepath.Join(dir, "input"+ext)
	if err := os.WriteFile(input, data, 0o600); err != nil {
		return nil, err
	}

	if animated {
		return convertAnimated(ctx, ffmpegPath, dir, input)
	}
	return convertStatic(ctx, ffmpegPath, dir, input)
}

func convertStatic(ctx context.Context, ffmpegPath, dir, input string) (*Result, error) {
	output := filepath.Join(dir, "sticker.webp")
	filter := scalePadFilter()

	for _, quality := range []int{80, 65, 50, 35, 25} {
		args := []string{
			"-hide_banner", "-loglevel", "error", "-y",
			"-i", input,
			"-vf", filter,
			"-frames:v", "1",
			"-an",
			"-c:v", "libwebp",
			"-lossless", "0",
			"-q:v", strconv.Itoa(quality),
			"-compression_level", "6",
			"-preset", "picture",
			output,
		}
		if err := runFFmpeg(ctx, ffmpegPath, args); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(output)
		if err != nil {
			return nil, err
		}
		if len(data) <= MaxStaticBytes {
			return &Result{Data: data, Animated: false}, nil
		}
	}
	return nil, ErrTooLarge
}

func convertAnimated(ctx context.Context, ffmpegPath, dir, input string) (*Result, error) {
	output := filepath.Join(dir, "sticker.webp")
	attempts := []struct {
		quality int
		fps     int
		seconds int
	}{
		{65, 12, 6},
		{50, 12, 6},
		{40, 10, 5},
		{30, 8, 4},
		{22, 6, 3},
	}

	for _, attempt := range attempts {
		filter := fmt.Sprintf("fps=%d,%s", attempt.fps, scalePadFilter())
		args := []string{
			"-hide_banner", "-loglevel", "error", "-y",
			"-i", input,
			"-t", strconv.Itoa(attempt.seconds),
			"-vf", filter,
			"-an",
			"-c:v", "libwebp_anim",
			"-lossless", "0",
			"-q:v", strconv.Itoa(attempt.quality),
			"-compression_level", "6",
			"-preset", "picture",
			"-loop", "0",
			output,
		}
		if err := runFFmpeg(ctx, ffmpegPath, args); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(output)
		if err != nil {
			return nil, err
		}
		if len(data) <= MaxAnimatedBytes {
			return &Result{Data: data, Animated: true}, nil
		}
	}
	return nil, ErrTooLarge
}

func scalePadFilter() string {
	return fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease:flags=lanczos,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=0x00000000,format=rgba",
		Size, Size, Size, Size,
	)
}

func runFFmpeg(ctx context.Context, ffmpegPath string, args []string) error {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		if msg != "" {
			return fmt.Errorf("ffmpeg: %w (%s)", err, msg)
		}
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return nil
}

func normalizeExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ".bin"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	switch ext {
	case ".jpeg", ".jpe":
		return ".jpg"
	case ".jpg", ".png", ".gif", ".webp", ".mp4", ".webm", ".mov", ".m4v", ".mkv":
		return ext
	default:
		return ".bin"
	}
}

func isAnimatedExt(ext string) bool {
	switch ext {
	case ".gif", ".mp4", ".webm", ".mov", ".m4v", ".mkv":
		return true
	}
	return false
}

// isAnimatedWebP walks RIFF chunks looking for the ANIM chunk.
func isAnimatedWebP(data []byte) bool {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return false
	}
	for off := 12; off+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		if string(data[off:off+4]) == "ANIM" {
			return true
		}
		off += 8 + size + size%2
	}
	return false
}
