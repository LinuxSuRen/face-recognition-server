package engine

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"

	"golang.org/x/image/webp"
)

// ErrUnsupportedImage marks uploads the decoder cannot read.
var ErrUnsupportedImage = errors.New("image is not a valid JPEG, PNG, WebP or GIF file")

// DecodeImage decodes uploaded bytes into the engine's RGB raster. The
// accepted formats mirror the reference service (JPEG, PNG, WebP) plus GIF,
// which the stdlib decoder provides for free.
func DecodeImage(content []byte) (*Image, error) {
	if len(content) == 0 {
		return nil, errors.New("image is empty")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return nil, ErrUnsupportedImage
	}
	// guard against decompression bombs; 80 MP covers any sane camera photo
	if config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > 80_000_000 {
		return nil, errors.New("image resolution exceeds the supported maximum")
	}
	reader := bytes.NewReader(content)
	var decoded image.Image
	if format == "webp" {
		decoded, err = webp.Decode(reader)
	} else {
		decoded, _, err = image.Decode(reader)
	}
	if err != nil {
		return nil, ErrUnsupportedImage
	}
	return FromGoImage(decoded), nil
}

// EncodeJPEG renders the image as JPEG for previews and evidence frames.
func EncodeJPEG(img *Image, quality int) ([]byte, error) {
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img.ToGoImage(), &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
