package image

import (
	"fmt"
	"image"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// PDFium converts Matte through the image's color space before undoing the
// preblended RGB samples (CPDF_DIB::StartLoadMask and bitmap compositing).
func removeMatte(pixels *image.NRGBA, matte *objects.Array, d, resources *objects.Dictionary) error {
	cs, err := colorSpace(d.GetObjectFor("ColorSpace"), resources, make(map[objects.Object]bool))
	if err != nil {
		return err
	}
	if matte.Len() != cs.components {
		return fmt.Errorf("image: invalid Matte components")
	}
	values := make([]float64, cs.components)
	for i := range values {
		n := objects.ToNumber(matte.GetDirectObjectAt(i))
		if n == nil {
			return fmt.Errorf("image: invalid Matte value")
		}
		values[i] = float64(n.GetNumber())
	}
	c := cs.rgb(values)
	background := []byte{c.R, c.G, c.B}
	for i := 0; i < len(pixels.Pix); i += 4 {
		a := float64(pixels.Pix[i+3]) / 255
		if a == 0 {
			continue
		}
		for channel := range 3 {
			pixels.Pix[i+channel] = unit((float64(pixels.Pix[i+channel])/255 - (1-a)*float64(background[channel])/255) / a)
		}
	}
	return nil
}
