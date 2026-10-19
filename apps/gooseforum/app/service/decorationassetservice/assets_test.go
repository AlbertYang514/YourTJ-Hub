package decorationassetservice

import (
	"bytes"
	"image"
	"image/color"
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
