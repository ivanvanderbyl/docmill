// Ported (minimally) from core/fpdfapi/page/cpdf_clippath.{h,cpp} @ pdfium
// 0db284a42.
//
// Clip-path manipulation is DEFERRABLE for first-pass text extraction: clip
// state does not change recorded glyph origins, and clip-mode text is rare. We
// keep a no-op placeholder so GraphicStates and SetGraphicStates have a
// target; ContentParser's checkClip is likewise a no-op, matching the
// deferral noted in the spec.
package page

// ClipPath is a stub for the clip-path state. It carries no path data in this
// face-less, text-only port.
type ClipPath struct{}
