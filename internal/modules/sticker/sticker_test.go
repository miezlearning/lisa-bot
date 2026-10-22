package sticker

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
	"time"
)

func TestConvertStaticImage(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg tidak tersedia")
	}

	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var input bytes.Buffer
	if err := png.Encode(&input, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	result, err := Convert(ctx, input.Bytes(), ".png", false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if result.Animated {
		t.Fatal("sticker statis terdeteksi animated")
	}
	if len(result.Data) > MaxStaticBytes {
		t.Fatalf("ukuran %d melebihi batas %d", len(result.Data), MaxStaticBytes)
	}
	assertWebP(t, result.Data)
	if isAnimatedWebP(result.Data) {
		t.Fatal("output statis mengandung chunk ANIM")
	}
}

func TestConvertAnimatedGIF(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg tidak tersedia")
	}

	input := buildGIF(t, 4)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	result, err := Convert(ctx, input, ".gif", true)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if !result.Animated {
		t.Fatal("sticker GIF tidak ditandai animated")
	}
	if len(result.Data) > MaxAnimatedBytes {
		t.Fatalf("ukuran %d melebihi batas %d", len(result.Data), MaxAnimatedBytes)
	}
	assertWebP(t, result.Data)
	if !isAnimatedWebP(result.Data) {
		t.Fatal("output tidak mengandung chunk ANIM")
	}
}

func buildGIF(t *testing.T, frames int) []byte {
	t.Helper()

	palette := color.Palette{
		color.RGBA{255, 0, 0, 255},
		color.RGBA{0, 0, 255, 255},
		color.RGBA{0, 255, 0, 255},
	}
	anim := &gif.GIF{LoopCount: 0}
	for i := 0; i < frames; i++ {
		frame := image.NewPaletted(image.Rect(0, 0, 240, 120), palette)
		for y := 0; y < 120; y++ {
			for x := 0; x < 240; x++ {
				frame.SetColorIndex(x, y, uint8((x/20+i)%len(palette)))
			}
		}
		anim.Image = append(anim.Image, frame)
		anim.Delay = append(anim.Delay, 10)
		anim.Disposal = append(anim.Disposal, gif.DisposalNone)
	}

	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, anim); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

func assertWebP(t *testing.T, data []byte) {
	t.Helper()
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		t.Fatal("output bukan file WebP yang valid")
	}
}
