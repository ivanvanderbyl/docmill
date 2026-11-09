package parser

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
)

// Decode reads the encoded image in Data. It detects the format from the bytes
// rather than trusting Format, Width, or Height. ExtractImages returns PNG or
// JPEG data; this method does not decode raw PDF image streams.
func (i ExtractedImage) Decode() (image.Image, error) {
	decoded, _, err := image.Decode(bytes.NewReader(i.Data))
	if err != nil {
		return nil, fmt.Errorf("decode extracted image: %w", err)
	}
	return decoded, nil
}

// PNG encodes the image as PNG, preserving decoded pixel values and alpha.
// It does not preserve source metadata such as EXIF or color profiles.
func (i ExtractedImage) PNG() ([]byte, error) {
	decoded, err := i.Decode()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, decoded); err != nil {
		return nil, fmt.Errorf("encode extracted image as PNG: %w", err)
	}
	return out.Bytes(), nil
}

// JPEG encodes the image as JPEG after compositing it onto a white background.
// Nil options use jpeg.DefaultQuality. Encoding is lossy, including when the
// source is JPEG, and does not preserve source metadata.
func (i ExtractedImage) JPEG(options *jpeg.Options) ([]byte, error) {
	decoded, err := i.Decode()
	if err != nil {
		return nil, err
	}
	opaque := image.NewRGBA(decoded.Bounds())
	draw.Draw(opaque, opaque.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(opaque, opaque.Bounds(), decoded, decoded.Bounds().Min, draw.Over)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, opaque, options); err != nil {
		return nil, fmt.Errorf("encode extracted image as JPEG: %w", err)
	}
	return out.Bytes(), nil
}
