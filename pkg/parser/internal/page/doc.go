// Package page is the PDF content-stream interpreter ported from PDFium's
// core/fpdfapi/page layer @ pdfium 0db284a42:
//
//   - cpdf_page / cpdf_pageobjectholder  -> Page / PageObjectHolder
//   - cpdf_allstates / cpdf_textstate    -> AllStates / TextState
//   - cpdf_contentparser                 -> ContentParser (the /Contents driver)
//   - cpdf_streamparser                  -> StreamParser (the content tokenizer)
//   - cpdf_streamcontentparser           -> StreamContentParser (the operator table)
//   - cpdf_textobject                    -> TextObject
//   - cpdf_form                          -> Form (Form XObject recursion)
//
// It loads a page, concatenates and decodes its /Contents, runs the
// content-stream interpreter, and produces the ordered list of text objects
// (and form objects, recursively) with each glyph's char code and origin.
//
// Faithfulness notes (carry-overs from the spec):
//
//   - All geometry is float32 and routes through the crt package, whose matrix
//     and point operators store every product in a local before summing to
//     defeat FMA and match PDFium's float math bit-for-bit.
//   - This is a FACE-LESS port: the font package has no FreeType backing, so
//     per-glyph bounding boxes (CPDF_SimpleFont::GetCharBBox) are unavailable.
//     The interpreter therefore computes each glyph ADVANCE/ORIGIN faithfully
//     (from GetCharWidthF + Tc/Tw/Tz + TJ kerning) but the resulting
//     TextObject Rect/OriginalRect is computed with a zero glyph bbox. The
//     textpage layer recomputes char boxes itself from TextMatrix/FontSize/
//     Font/CharCode/Origin, which ARE faithful, so this does not affect text
//     extraction. See getCharBBox in textobject.go.
//
// See plan 009 Phase F.
package page
