// Code generated from core/fxge/cfx_fontmapper.cpp @ pdfium 0db284a42. DO NOT EDIT.
package font

// standardFont mirrors CFX_FontMapper::StandardFont (Base-14 index).
type standardFont int

const (
	stdCourier standardFont = iota
	stdCourierBold
	stdCourierBoldOblique
	stdCourierOblique
	stdHelvetica
	stdHelveticaBold
	stdHelveticaBoldOblique
	stdHelveticaOblique
	stdTimes
	stdTimesBold
	stdTimesBoldOblique
	stdTimesOblique
	stdSymbol
	stdDingbats
)

// base14FontNames is kBase14FontNames (canonical names indexed by StandardFont).
var base14FontNames = [14]string{
	"Courier",
	"Courier-Bold",
	"Courier-BoldOblique",
	"Courier-Oblique",
	"Helvetica",
	"Helvetica-Bold",
	"Helvetica-BoldOblique",
	"Helvetica-Oblique",
	"Times-Roman",
	"Times-Bold",
	"Times-BoldItalic",
	"Times-Italic",
	"Symbol",
	"ZapfDingbats",
}

type altFontName struct {
	name  string
	index standardFont
}

// kAltFontNames is the Base-14 alias table (matched case-insensitively).
var kAltFontNames = []altFontName{
	{"Arial", 4},
	{"Arial,Bold", 5},
	{"Arial,BoldItalic", 6},
	{"Arial,Italic", 7},
	{"Arial-Bold", 5},
	{"Arial-BoldItalic", 6},
	{"Arial-BoldItalicMT", 6},
	{"Arial-BoldMT", 5},
	{"Arial-Italic", 7},
	{"Arial-ItalicMT", 7},
	{"ArialBold", 5},
	{"ArialBoldItalic", 6},
	{"ArialItalic", 7},
	{"ArialMT", 4},
	{"ArialMT,Bold", 5},
	{"ArialMT,BoldItalic", 6},
	{"ArialMT,Italic", 7},
	{"ArialRoundedMTBold", 5},
	{"Courier", 0},
	{"Courier,Bold", 1},
	{"Courier,BoldItalic", 2},
	{"Courier,Italic", 3},
	{"Courier-Bold", 1},
	{"Courier-BoldOblique", 2},
	{"Courier-Oblique", 3},
	{"CourierBold", 1},
	{"CourierBoldItalic", 2},
	{"CourierItalic", 3},
	{"CourierNew", 0},
	{"CourierNew,Bold", 1},
	{"CourierNew,BoldItalic", 2},
	{"CourierNew,Italic", 3},
	{"CourierNew-Bold", 1},
	{"CourierNew-BoldItalic", 2},
	{"CourierNew-Italic", 3},
	{"CourierNewBold", 1},
	{"CourierNewBoldItalic", 2},
	{"CourierNewItalic", 3},
	{"CourierNewPS-BoldItalicMT", 2},
	{"CourierNewPS-BoldMT", 1},
	{"CourierNewPS-ItalicMT", 3},
	{"CourierNewPSMT", 0},
	{"CourierStd", 0},
	{"CourierStd-Bold", 1},
	{"CourierStd-BoldOblique", 2},
	{"CourierStd-Oblique", 3},
	{"Helvetica", 4},
	{"Helvetica,Bold", 5},
	{"Helvetica,BoldItalic", 6},
	{"Helvetica,Italic", 7},
	{"Helvetica-Bold", 5},
	{"Helvetica-BoldItalic", 6},
	{"Helvetica-BoldOblique", 6},
	{"Helvetica-Italic", 7},
	{"Helvetica-Oblique", 7},
	{"HelveticaBold", 5},
	{"HelveticaBoldItalic", 6},
	{"HelveticaItalic", 7},
	{"Symbol", 12},
	{"SymbolMT", 12},
	{"Times-Bold", 9},
	{"Times-BoldItalic", 10},
	{"Times-Italic", 11},
	{"Times-Roman", 8},
	{"TimesBold", 9},
	{"TimesBoldItalic", 10},
	{"TimesItalic", 11},
	{"TimesNewRoman", 8},
	{"TimesNewRoman,Bold", 9},
	{"TimesNewRoman,BoldItalic", 10},
	{"TimesNewRoman,Italic", 11},
	{"TimesNewRoman-Bold", 9},
	{"TimesNewRoman-BoldItalic", 10},
	{"TimesNewRoman-Italic", 11},
	{"TimesNewRomanBold", 9},
	{"TimesNewRomanBoldItalic", 10},
	{"TimesNewRomanItalic", 11},
	{"TimesNewRomanPS", 8},
	{"TimesNewRomanPS-Bold", 9},
	{"TimesNewRomanPS-BoldItalic", 10},
	{"TimesNewRomanPS-BoldItalicMT", 10},
	{"TimesNewRomanPS-BoldMT", 9},
	{"TimesNewRomanPS-Italic", 11},
	{"TimesNewRomanPS-ItalicMT", 11},
	{"TimesNewRomanPSMT", 8},
	{"TimesNewRomanPSMT,Bold", 9},
	{"TimesNewRomanPSMT,BoldItalic", 10},
	{"TimesNewRomanPSMT,Italic", 11},
	{"ZapfDingbats", 13},
}
