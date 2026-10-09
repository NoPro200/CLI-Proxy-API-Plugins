package executor

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestFitEditImageShrinksOnlyOversizedImages(t *testing.T) {
	encode := func(width, height int, asJPEG bool) []byte {
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := range height {
			for x := range width {
				img.SetRGBA(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
			}
		}
		var out bytes.Buffer
		if asJPEG {
			_ = jpeg.Encode(&out, img, nil)
		} else {
			_ = png.Encode(&out, img)
		}
		return out.Bytes()
	}

	// Portrait like the phone photo Venice rejected (4592x8160), scaled down to keep the test fast.
	tall, err := fitEditImage(imageUpload{Name: "photo.jpg", ContentType: "image/jpeg", Data: encode(90, 8160, true)})
	if err != nil {
		t.Fatalf("fitEditImage error: %v", err)
	}
	config, format, _ := image.DecodeConfig(bytes.NewReader(tall.Data))
	if config.Height != maxEditImageSide || config.Width != 48 || format != "jpeg" || tall.ContentType != "image/jpeg" || tall.Name != "photo.jpg" {
		t.Fatalf("shrunk = %dx%d %s %s %s", config.Width, config.Height, format, tall.ContentType, tall.Name)
	}
	pixel, _, _ := image.Decode(bytes.NewReader(tall.Data))
	if r, g, b, _ := pixel.At(10, 10).RGBA(); r>>8 < 190 || g>>8 < 90 || b>>8 > 70 {
		t.Fatalf("averaged colour drifted: %d %d %d", r>>8, g>>8, b>>8)
	}

	wide, err := fitEditImage(imageUpload{Name: "mask", ContentType: "image/png", Data: encode(8640, 20, false)})
	config, format, _ = image.DecodeConfig(bytes.NewReader(wide.Data))
	if err != nil || config.Width != maxEditImageSide || config.Height != 10 || format != "png" || wide.Name != "mask.png" {
		t.Fatalf("shrunk png = %dx%d %s %s, %v", config.Width, config.Height, format, wide.Name, err)
	}

	small := imageUpload{Name: "small.png", ContentType: "image/png", Data: encode(300, 300, false)}
	if kept, _ := fitEditImage(small); !bytes.Equal(kept.Data, small.Data) {
		t.Fatal("image within the limit was re-encoded")
	}
}
