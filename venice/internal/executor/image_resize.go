package executor

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // register the decoder for image.Decode
	"image/jpeg"
	"image/png"
	"path"
	"strings"
)

// maxEditImageSide mirrors the web app, which shrinks uploads to 4320 px on the
// long side; Venice rejects larger inputs with IMAGE_TOO_LARGE (max 7680×4320).
const maxEditImageSide = 4320

// maxDecodePixels bounds what fitEditImage decodes. Decoding allocates per
// declared pixel, so a tiny file claiming huge dimensions could otherwise
// exhaust the memory of the host process; 60 MP still covers 50 MP cameras.
const maxDecodePixels = 60_000_000

// fitEditImage shrinks an upload whose long side exceeds what Venice accepts
// and leaves every other upload untouched.
// shortcut: EXIF orientation is dropped when an image is shrunk; add rotation if sideways edits show up.
func fitEditImage(upload imageUpload) (imageUpload, error) {
	config, format, errConfig := image.DecodeConfig(bytes.NewReader(upload.Data))
	if errConfig != nil || max(config.Width, config.Height) <= maxEditImageSide {
		// Formats Go cannot read, such as WebP, go to Venice as they are.
		return upload, nil
	}
	if config.Width > maxDecodePixels/max(config.Height, 1) {
		return imageUpload{}, badRequest("%s is %dx%d px; images above %d MP are rejected, send a smaller one", upload.Name, config.Width, config.Height, maxDecodePixels/1_000_000)
	}
	src, _, errDecode := image.Decode(bytes.NewReader(upload.Data))
	if errDecode != nil {
		return imageUpload{}, badRequest("decode %s: %v", upload.Name, errDecode)
	}
	scale := float64(maxEditImageSide) / float64(max(config.Width, config.Height))
	dst := downscale(src, max(1, int(float64(config.Width)*scale+0.5)), max(1, int(float64(config.Height)*scale+0.5)))
	var out bytes.Buffer
	contentType, ext := "image/png", ".png"
	var errEncode error
	if format == "jpeg" {
		contentType, ext = "image/jpeg", ".jpg"
		errEncode = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 92})
	} else {
		// PNG keeps the transparency that masks and cut-outs rely on.
		errEncode = png.Encode(&out, dst)
	}
	if errEncode != nil {
		return imageUpload{}, errEncode
	}
	name := strings.TrimSuffix(upload.Name, path.Ext(upload.Name)) + ext
	return imageUpload{Name: name, ContentType: contentType, Data: out.Bytes()}, nil
}

// downscale averages each source block into one pixel. A box filter is enough
// for shrinking and reads every source pixel once.
func downscale(src image.Image, width, height int) *image.RGBA {
	bounds := src.Bounds()
	pixels, ok := src.(image.RGBA64Image)
	if !ok {
		rgba := image.NewRGBA(bounds)
		draw.Draw(rgba, bounds, src, bounds.Min, draw.Src)
		pixels = rgba
	}
	sw, sh := bounds.Dx(), bounds.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for dy := range height {
		y0 := dy * sh / height
		y1 := max((dy+1)*sh/height, y0+1)
		for dx := range width {
			x0 := dx * sw / width
			x1 := max((dx+1)*sw/width, x0+1)
			var r, g, b, a uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					c := pixels.RGBA64At(bounds.Min.X+x, bounds.Min.Y+y)
					r, g, b, a = r+uint64(c.R), g+uint64(c.G), b+uint64(c.B), a+uint64(c.A)
				}
			}
			n := uint64((y1 - y0) * (x1 - x0))
			dst.SetRGBA(dx, dy, color.RGBA{R: uint8(r / n >> 8), G: uint8(g / n >> 8), B: uint8(b / n >> 8), A: uint8(a / n >> 8)})
		}
	}
	return dst
}
