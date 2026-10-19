package decorationassetservice

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"math"
	"testing"
)

func TestUploadedBadgesMatchSystemOpticalRadius(t *testing.T) {
	for _, rect := range []image.Rectangle{image.Rect(140, 30, 240, 130), image.Rect(40, 60, 340, 90), image.Rect(170, 10, 190, 270)} {
		src := image.NewNRGBA(image.Rect(0, 0, 400, 300))
		draw.Draw(src, rect, image.NewUniform(color.NRGBA{R: 240, G: 80, A: 255}), image.Point{}, draw.Src)
		var data bytes.Buffer
		if err := png.Encode(&data, src); err != nil {
			t.Fatal(err)
		}
		normalized, name, err := prepareArtwork(t.Context(), data.Bytes(), "logo.png", false)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(normalized))
		if err != nil {
			t.Fatal(err)
		}
		if name != "logo.png" || img.Bounds() != image.Rect(0, 0, 512, 512) {
			t.Fatalf("wrong normalized canvas: %s %v", name, img.Bounds())
		}
		ink, radius := measureBadgeInk(img)
		center := float64(ink.Min.X+ink.Max.X) / 2
		if math.Abs(center-256) > 1 || math.Abs(float64(ink.Min.Y+ink.Max.Y)/2-256) > 1 {
			t.Fatalf("not centered: %v", ink)
		}
		if math.Abs(radius-512*10.0/24) > 2 {
			t.Fatalf("radius %f differs from system badge target", radius)
		}
		if math.Abs(float64(ink.Dx())/float64(ink.Dy())-float64(rect.Dx())/float64(rect.Dy())) > 0.15 {
			t.Fatalf("distorted artwork: %v -> %v", rect, ink)
		}
	}
}

func measureBadgeInk(img image.Image) (image.Rectangle, float64) {
	var ink image.Rectangle
	radius := 0.0
	b := img.Bounds()
	cx, cy := float64(b.Dx())/2, float64(b.Dy())/2
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 257 {
				ink = ink.Union(image.Rect(x, y, x+1, y+1))
				radius = max(radius, math.Hypot(float64(x)+.5-cx, float64(y)+.5-cy))
			}
		}
	}
	return ink, radius
}

func TestAnimatedBadgeUsesOneViewportAndKeepsPlayback(t *testing.T) {
	palette := color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	frames := []*image.Paletted{image.NewPaletted(image.Rect(0, 0, 100, 100), palette), image.NewPaletted(image.Rect(0, 0, 100, 100), palette)}
	draw.Draw(frames[0], image.Rect(10, 40, 20, 60), image.NewUniform(palette[1]), image.Point{}, draw.Src)
	draw.Draw(frames[1], image.Rect(80, 40, 90, 60), image.NewUniform(palette[2]), image.Point{}, draw.Src)
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, &gif.GIF{Image: frames, Delay: []int{3, 7}, LoopCount: 4, Disposal: []byte{gif.DisposalBackground, gif.DisposalBackground}}); err != nil {
		t.Fatal(err)
	}
	normalized, name, err := prepareArtwork(t.Context(), data.Bytes(), "moving.gif", false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := gif.DecodeAll(bytes.NewReader(normalized))
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Width == 100 || got.Config.Width != got.Config.Height {
		t.Fatal("GIF was not normalized to a shared square canvas")
	}
	if name != "moving.gif" || len(got.Image) != 2 || got.LoopCount != 4 || got.Delay[0] != 3 || got.Delay[1] != 7 {
		t.Fatal("playback metadata changed")
	}
	a, _ := measureBadgeInk(got.Image[0])
	b, _ := measureBadgeInk(got.Image[1])
	for i, rect := range []image.Rectangle{a, b} {
		r, g, blue, alpha := got.Image[i].At(rect.Min.X+rect.Dx()/2, rect.Min.Y+rect.Dy()/2).RGBA()
		if alpha != 65535 || g != 0 || (i == 0 && (r != 65535 || blue != 0)) || (i == 1 && (blue != 65535 || r != 0)) {
			t.Fatal("GIF logo colors changed during normalization")
		}
	}

	if a.Dx() != b.Dx() || a.Dy() != b.Dy() || a.Max.X >= b.Min.X {
		t.Fatalf("animation viewport shifted or frames resized independently: %v %v", a, b)
	}
}

func TestBlankBadgeIsRejectedAndAvatarFramesAreUnchanged(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 20, 20))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareArtwork(t.Context(), data.Bytes(), "empty.png", false); err == nil {
		t.Fatal("accepted empty badge artwork")
	}
	original := bytes.Clone(data.Bytes())
	frame, _, err := prepareArtwork(t.Context(), data.Bytes(), "frame.png", true)
	if err != nil || !bytes.Equal(frame, original) {
		t.Fatal("badge normalization changed avatar-frame artwork")
	}
}

func TestThinBadgeStaysVisible(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4096, 1))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	data, _, err := prepareArtwork(t.Context(), out.Bytes(), "line.png", false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	ink, _ := measureBadgeInk(got)
	if ink.Empty() || ink.Dy() != 1 {
		t.Fatalf("thin artwork vanished: %v", ink)
	}
}

func TestPartialGIFFramesCannotExceedCompositingBudget(t *testing.T) {
	palette := color.Palette{color.Transparent, color.White}
	// 帧数由合成像素预算推导：1×1 的加密帧也要能撞破 frames × canvas 预算（issue 回归点）。
	frames := animatedArtworkMaxPixels/(1024*1024) + 1
	images := make([]*image.Paletted, frames)
	for i := range images {
		images[i] = image.NewPaletted(image.Rect(0, 0, 1, 1), palette)
	}
	var out bytes.Buffer
	if err := gif.EncodeAll(&out, &gif.GIF{Image: images, Delay: make([]int, len(images)), Config: image.Config{Width: 1024, Height: 1024, ColorModel: palette}}); err != nil {
		t.Fatal(err)
	}
	if gifBudget(out.Bytes()) {
		t.Fatal("small encoded patches bypassed logical-canvas allocation budget")
	}
}
