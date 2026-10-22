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
	// thickRing 的开孔直径只占画布 10%，低于 30% 下限。
	thickRing := func(size int) []byte {
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		c := float64(size) / 2
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				dx, dy := float64(x)-c, float64(y)-c
				if dx*dx+dy*dy > (c*0.1)*(c*0.1) {
					img.Set(x, y, color.NRGBA{R: 30, G: 90, B: 60, A: 255})
				}
			}
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
		// 轻微非方形（±15% 内）放行：渲染端 contain 会把它装进正方框，不会错位。
		{"near square rectangle", makePNG(21, 20, false), true},
		{"aspect ratio at limit", makePNG(85, 100, false), true},
		{"aspect ratio beyond limit", makePNG(100, 84, false), false},
		{"transparent single pixel", makePNG(1, 1, false), true},
		{"opaque single pixel", makePNG(1, 1, true), false},
		{"opaque tiny center", makePNG(2, 2, true), false},
		{"strongly non-square rectangle", makePNG(20, 40, false), false},
		{"opaque center", makePNG(20, 20, true), false},
		{"thick ring without usable opening", thickRing(40), false},
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

func TestArtworkCircularOpening(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alpha uint8
		x, y  int
		valid bool
	}{
		{"circular opening with thick ring", 0, 54, 52, true},
		{"opaque pixel between sampling rays", 255, 54, 52, false},
		{"opaque center", 255, 50, 50, false},
		{"allowed opacity", 1, 54, 52, true},
		{"excess opacity", 2, 54, 52, false},
		{"ornament outside minimum opening", 255, 67, 50, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := image.NewNRGBA(image.Rect(0, 0, 100, 100))
			for y := 0; y < 100; y++ {
				for x := 0; x < 100; x++ {
					dx, dy := float64(x)-49.5, float64(y)-49.5
					if dx*dx+dy*dy >= 19*19 {
						img.SetNRGBA(x, y, color.NRGBA{A: 255})
					}
				}
			}
			img.SetNRGBA(tc.x, tc.y, color.NRGBA{A: tc.alpha})
			var data bytes.Buffer
			if err := png.Encode(&data, img); err != nil {
				t.Fatal(err)
			}
			if err := ValidateArtwork(data.Bytes(), "image/png"); (err == nil) != tc.valid {
				t.Fatalf("ValidateArtwork() = %v, want valid %v", err, tc.valid)
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
		{`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 85 100"><rect width="10" height="10" fill="red"/></svg>`, true, true},
		{`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 84 100"><rect width="10" height="10" fill="red"/></svg>`, true, false},
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
