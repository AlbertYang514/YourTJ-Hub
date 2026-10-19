package decorationassetservice

import (
	"bytes"
	"cmp"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"math"
	"path"
	"slices"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
)

// Uploaded artwork shares the system SVGs' ink radius (MADR 0054), not their source canvas margins.
const badgeInkRadius = 10.0 / 24

func normalizeBadge(ctx context.Context, data []byte, filename, contentType string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var animation *gif.GIF
	var still image.Image
	var err error
	if contentType == "image/gif" {
		animation, err = gif.DecodeAll(bytes.NewReader(data))
	} else {
		still, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, "", ErrInvalid
	}
	visit := func(fn func(image.Image) error) error {
		if animation != nil {
			return visitGIF(animation, fn)
		}
		return fn(still)
	}
	var ink image.Rectangle
	if err := visit(func(img image.Image) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 257 {
					ink = ink.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		return nil
	}); err != nil {
		return nil, "", err
	}
	if ink.Empty() {
		return nil, "", ErrInvalid
	}
	cx, cy := float64(ink.Min.X+ink.Max.X)/2, float64(ink.Min.Y+ink.Max.Y)/2
	radius := 0.0
	if err := visit(func(img image.Image) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		b := img.Bounds().Intersect(ink)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 257 {
					radius = max(radius, math.Hypot(math.Abs(float64(x)+.5-cx)+.5, math.Abs(float64(y)+.5-cy)+.5))
				}
			}
		}
		return nil
	}); err != nil {
		return nil, "", err
	}
	edge := 512
	if animation != nil {
		edge = min(256, int(math.Sqrt(float64(16*1024*1024/len(animation.Image)))))
	}
	scale := float64(edge) * badgeInkRadius / radius
	// One viewport for the entire animation preserves intentional motion and avoids per-frame zooming.
	destination := image.Rect(int(math.Round(float64(edge)/2+(float64(ink.Min.X)-cx)*scale)), int(math.Round(float64(edge)/2+(float64(ink.Min.Y)-cy)*scale)), int(math.Round(float64(edge)/2+(float64(ink.Max.X)-cx)*scale)), int(math.Round(float64(edge)/2+(float64(ink.Max.Y)-cy)*scale)))
	if destination.Dx() < 1 {
		destination.Max.X = destination.Min.X + 1
	}
	if destination.Dy() < 1 {
		destination.Max.Y = destination.Min.Y + 1
	}
	var output bytes.Buffer
	if animation == nil {
		canvas := image.NewNRGBA(image.Rect(0, 0, edge, edge))
		xdraw.CatmullRom.Scale(canvas, destination, still, ink, draw.Src, nil)
		if err := png.Encode(&output, canvas); err != nil {
			return nil, "", err
		}
		return output.Bytes(), strings.TrimSuffix(filename, path.Ext(filename)) + ".png", nil
	}
	fitted := &gif.GIF{Delay: animation.Delay, LoopCount: animation.LoopCount, Config: image.Config{Width: edge, Height: edge}}
	if err := visit(func(img image.Image) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		canvas := image.NewNRGBA(image.Rect(0, 0, edge, edge))
		// Nearest-neighbor preserves the original GIF colors instead of adding interpolation colors.
		xdraw.NearestNeighbor.Scale(canvas, destination, img, ink, draw.Src, nil)
		frame := image.NewPaletted(canvas.Bounds(), badgePalette(canvas))
		draw.FloydSteinberg.Draw(frame, frame.Bounds(), canvas, image.Point{})
		fitted.Image = append(fitted.Image, frame)
		fitted.Disposal = append(fitted.Disposal, gif.DisposalBackground)
		return nil
	}); err != nil {
		return nil, "", err
	}
	if err := gif.EncodeAll(&output, fitted); err != nil {
		return nil, "", err
	}
	return output.Bytes(), filename, nil
}

// Preserve the most-used source colors exactly and reserve transparency for the normalized padding.
func badgePalette(img *image.NRGBA) color.Palette {
	counts := map[color.NRGBA]int{}
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			c := img.NRGBAAt(x, y)
			if c.A > 1 {
				c.A = 255
				counts[c]++
			}
		}
	}
	colors := make([]color.NRGBA, 0, len(counts))
	for c := range counts {
		colors = append(colors, c)
	}
	slices.SortFunc(colors, func(a, b color.NRGBA) int {
		if counts[a] != counts[b] {
			return counts[b] - counts[a]
		}
		return cmp.Or(cmp.Compare(a.R, b.R), cmp.Compare(a.G, b.G), cmp.Compare(a.B, b.B))
	})
	palette := color.Palette{color.Transparent}
	for _, c := range colors[:min(255, len(colors))] {
		palette = append(palette, c)
	}
	return palette
}
