package decorationassetservice

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

func TestValidateArtwork(t *testing.T) {
	makePNG := func(width, height int, centerOpaque bool) []byte {
		img := image.NewNRGBA(image.Rect(0, 0, width, height))
		if centerOpaque {
			img.Set(width/2, height/2, color.NRGBA{R: 255, A: 255})
		}
		var out bytes.Buffer
		if err := png.Encode(&out, img); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	for _, tc := range []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"transparent square", makePNG(20, 20, false), true},
		{"rectangle", makePNG(20, 21, false), false},
		{"opaque center", makePNG(20, 20, true), false},
		{"damaged", []byte("not an image"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateArtwork(tc.data, "image/png")
			if (err == nil) != tc.valid {
				t.Fatalf("ValidateArtwork() error = %v, want valid %v", err, tc.valid)
			}
		})
	}
}

func TestAnimatedFrameValidatesEveryCompositedFrame(t *testing.T) {
	palette := color.Palette{color.Transparent, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 20, 20), palette)
	second := image.NewPaletted(image.Rect(0, 0, 20, 20), palette)
	for _, opaque := range []bool{false, true} {
		if opaque {
			second.SetColorIndex(10, 10, 1)
		}
		var data bytes.Buffer
		if err := gif.EncodeAll(&data, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{1, 1}}); err != nil {
			t.Fatal(err)
		}
		err := ValidateArtwork(data.Bytes(), "image/gif")
		if (err == nil) == opaque {
			t.Fatalf("opaque=%v: %v", opaque, err)
		}
	}
}
func TestSVGArtworkIsRasterizedBeforeValidation(t *testing.T) {
	for _, tc := range []struct {
		svg          string
		frame, valid bool
	}{
		{`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="10" height="10" fill="red"/></svg>`, true, true},
		{`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100" fill="red"/></svg>`, true, false},
		{`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100" fill="red"/></svg>`, false, true},
		{`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><style>.ring{fill:red}</style><rect class="ring" width="3" height="3"/></svg>`, true, true},
		{`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><style>@import url(file:///etc/passwd)</style></svg>`, false, false},
		{`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`, false, false},
		{`<svg xmlns="http://www.w3.org/2000/svg"><image href="file:///etc/passwd"/></svg>`, false, false},
	} {
		data, name, err := prepareArtwork(t.Context(), []byte(tc.svg), "art.svg", tc.frame)
		if (err == nil) != tc.valid {
			t.Fatalf("valid=%v frame=%v: %v", tc.valid, tc.frame, err)
		}
		if err == nil {
			if name != "art.png" {
				t.Fatal(name)
			}
			if _, err := png.Decode(bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestGIFDisposalAndBudget(t *testing.T) {
	palette := color.Palette{color.Transparent, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 20, 20), palette)
	// A later sub-frame must be positioned on the logical canvas, not checked in isolation.
	second := image.NewPaletted(image.Rect(9, 9, 11, 11), palette)
	second.SetColorIndex(9, 9, 1)
	var out bytes.Buffer
	if err := gif.EncodeAll(&out, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{1, 1}, Disposal: []byte{gif.DisposalPrevious, gif.DisposalBackground}, Config: image.Config{Width: 20, Height: 20, ColorModel: palette}}); err != nil {
		t.Fatal(err)
	}
	if ValidateArtwork(out.Bytes(), "image/gif") == nil {
		t.Fatal("accepted off-center opaque animation sub-frame")
	}
	if validateRaster(out.Bytes(), "image/gif", false) != nil {
		t.Fatal("badge inherited transparent-center requirement")
	}
	images := make([]*image.Paletted, 301)
	delays := make([]int, 301)
	for i := range images {
		images[i] = first
	}
	out.Reset()
	if err := gif.EncodeAll(&out, &gif.GIF{Image: images, Delay: delays}); err != nil {
		t.Fatal(err)
	}
	if validateRaster(out.Bytes(), "image/gif", false) == nil {
		t.Fatal("accepted excessive decoded frames")
	}
}

func TestGIFLogicalBackgroundCannotCoverAvatar(t *testing.T) {
	palette := color.Palette{color.White}
	img := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, &gif.GIF{Image: []*image.Paletted{img}, Delay: []int{1}, Config: image.Config{Width: 20, Height: 20, ColorModel: palette}}); err != nil {
		t.Fatal(err)
	}
	if ValidateArtwork(data.Bytes(), "image/gif") == nil {
		t.Fatal("accepted opaque logical background behind partial frame")
	}
}
