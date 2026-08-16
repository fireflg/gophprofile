package imageutil_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/pkg/imageutil"
)

func sampleImage(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}

	return img
}

func TestEncodeSupportedFormats(t *testing.T) {
	tests := map[string]string{
		imageutil.MimeJPEG: "jpeg",
		imageutil.MimePNG:  "png",
		imageutil.MimeWebP: "webp",
	}

	for mime, format := range tests {
		t.Run(format, func(t *testing.T) {
			require.True(t, imageutil.CanEncode(mime))

			var buf bytes.Buffer
			require.NoError(t, imageutil.Encode(&buf, sampleImage(64, 48), mime))

			cfg, decoded, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
			require.NoError(t, err)
			require.Equal(t, format, decoded)
			require.Equal(t, 64, cfg.Width)
			require.Equal(t, 48, cfg.Height)
		})
	}
}

func TestEncodeRejectsUnknownFormat(t *testing.T) {
	require.False(t, imageutil.CanEncode("image/gif"))
	require.Error(t, imageutil.Encode(&bytes.Buffer{}, sampleImage(8, 8), "image/gif"))
}

func TestEncodeWebPIsLossless(t *testing.T) {
	source := sampleImage(32, 32)

	var buf bytes.Buffer
	require.NoError(t, imageutil.Encode(&buf, source, imageutil.MimeWebP))

	decoded, format, err := imageutil.Decode(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Equal(t, "webp", format)

	for y := range 32 {
		for x := range 32 {
			wantR, wantG, wantB, wantA := source.At(x, y).RGBA()
			gotR, gotG, gotB, gotA := decoded.At(x, y).RGBA()

			require.Equal(t,
				[4]uint32{wantR, wantG, wantB, wantA},
				[4]uint32{gotR, gotG, gotB, gotA},
				"pixel %d,%d", x, y)
		}
	}
}

func TestDecodeWebP(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, imageutil.Encode(&buf, sampleImage(20, 10), imageutil.MimeWebP))

	require.Equal(t, imageutil.MimeWebP, imageutil.DetectMIME(buf.Bytes()))

	width, height, err := imageutil.DecodeConfig(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Equal(t, 20, width)
	require.Equal(t, 10, height)
}

func TestThumbnailMIME(t *testing.T) {
	require.Equal(t, imageutil.MimePNG, imageutil.ThumbnailMIME(imageutil.MimePNG))
	require.Equal(t, imageutil.MimeWebP, imageutil.ThumbnailMIME(imageutil.MimeWebP))
	require.Equal(t, imageutil.MimeJPEG, imageutil.ThumbnailMIME(imageutil.MimeJPEG))
	require.Equal(t, imageutil.MimeJPEG, imageutil.ThumbnailMIME("image/gif"))
	require.Equal(t, imageutil.MimeJPEG, imageutil.ThumbnailMIME(""))
}

func TestNormalizeFormat(t *testing.T) {
	mime, ok := imageutil.NormalizeFormat("WEBP")
	require.True(t, ok)
	require.Equal(t, imageutil.MimeWebP, mime)

	mime, ok = imageutil.NormalizeFormat("")
	require.True(t, ok)
	require.Empty(t, mime)

	_, ok = imageutil.NormalizeFormat("tiff")
	require.False(t, ok)
}

func TestThumbnailKeepsSquareShape(t *testing.T) {
	thumb := imageutil.Thumbnail(sampleImage(640, 480), 100, 100)

	require.Equal(t, 100, thumb.Bounds().Dx())
	require.Equal(t, 100, thumb.Bounds().Dy())
}

func TestEncodeJPEGAndPNGStayDecodable(t *testing.T) {
	var jpegBuf bytes.Buffer
	require.NoError(t, imageutil.Encode(&jpegBuf, sampleImage(16, 16), imageutil.MimeJPEG))

	_, err := jpeg.Decode(bytes.NewReader(jpegBuf.Bytes()))
	require.NoError(t, err)

	var pngBuf bytes.Buffer
	require.NoError(t, imageutil.Encode(&pngBuf, sampleImage(16, 16), imageutil.MimePNG))

	_, err = png.Decode(bytes.NewReader(pngBuf.Bytes()))
	require.NoError(t, err)
}
