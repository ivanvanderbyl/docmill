//go:build !pdfium_cgo || !cgo

// Command formsviz needs the pdfium_cgo build tag (and a PDFium install
// reachable through pkg-config); this stub keeps untagged builds green.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "formsviz was built without PDFium support; rebuild with:")
	fmt.Fprintln(os.Stderr, "  task formsviz:build   (or: CGO_ENABLED=1 go build -tags pdfium_cgo ./cmd/formsviz)")
	os.Exit(2)
}
