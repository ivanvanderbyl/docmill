// Additional SyntaxParser entry points the parser layer (Phase D) needs:
// GetDirectNum, ReadBlock, BackwardsSearchToWord, CharAt, strict indirect-object
// parsing, and exported number helpers. Ported from the matching
// CPDF_SyntaxParser methods @ pdfium 0db284a42.
package syntax

import (
	"bytes"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
)

// GetDirectNum reads the next token and returns its unsigned value, or 0 if it
// is not numeric (CPDF_SyntaxParser::GetDirectNum).
func (p *SyntaxParser) GetDirectNum() uint32 {
	p.getNextWordInternal()
	if !p.wordIsNu {
		return 0
	}
	return atoui(p.word())
}

// ToNextWord skips whitespace/comments, leaving the cursor on the next token.
func (p *SyntaxParser) ToNextWord() { p.toNextWord() }

// ReadBlock copies the next len(block) bytes at the cursor into block, advancing
// the cursor; it returns false if fewer bytes remain.
func (p *SyntaxParser) ReadBlock(block []byte) bool {
	i := p.pos + p.headerOffset
	if i < 0 || i+len(block) > len(p.buf) {
		return false
	}
	copy(block, p.buf[i:i+len(block)])
	p.pos += len(block)
	return true
}

// CharAt returns the byte at document position pos.
func (p *SyntaxParser) CharAt(pos int) (byte, bool) { return p.getCharAt(pos) }

// SetReadBufferSize is a no-op (the Go port holds the whole file); kept for
// parity with RebuildCrossRef's buffer-size dance.
func (p *SyntaxParser) SetReadBufferSize(int) {}

// BackwardsSearchToWord searches backward from the cursor for a whole-word match
// of word, not crossing cursor-limit (limit 0 = no limit). On a match it sets
// the cursor to the match start and returns true.
func (p *SyntaxParser) BackwardsSearchToWord(word string, limit int) bool {
	if word == "" {
		return false
	}
	hi := min(p.pos+p.headerOffset, len(p.buf))
	lo := 0
	if limit > 0 {
		lo = max(hi-limit, 0)
	}
	if lo < p.headerOffset {
		lo = p.headerOffset
	}
	w := []byte(word)
	end := hi
	for end >= lo+len(w) {
		idx := bytes.LastIndex(p.buf[lo:end], w)
		if idx < 0 {
			return false
		}
		matchPhys := lo + idx
		docPos := matchPhys - p.headerOffset
		if docPos >= 0 && p.isWholeWord(docPos, len(p.buf), word, false) {
			p.pos = docPos
			return true
		}
		end = matchPhys + len(w) - 1
	}
	return false
}

func (p *SyntaxParser) getIndirectObjectWithType(holder objects.IndirectObjectHolder, pt parseType) objects.Object {
	saved := p.GetPos()
	word, isNum := p.GetNextWord()
	if !isNum || len(word) == 0 {
		p.SetPos(saved)
		return nil
	}
	objnum := atoui(word)
	word, isNum = p.GetNextWord()
	if !isNum || len(word) == 0 {
		p.SetPos(saved)
		return nil
	}
	gennum := atoui(word)
	if p.GetKeyword() != "obj" {
		p.SetPos(saved)
		return nil
	}
	obj := p.getObjectBodyInternal(holder, pt)
	if obj != nil {
		obj.SetObjNum(objnum)
		obj.SetGenNum(gennum)
	}
	return obj
}

// GetIndirectObjectStrict parses "N G obj <body>" in strict mode (used by
// RebuildCrossRef to extract a clean stream object).
func (p *SyntaxParser) GetIndirectObjectStrict(holder objects.IndirectObjectHolder) objects.Object {
	return p.getIndirectObjectWithType(holder, parseStrict)
}

// Atoui exposes FXSYS_atoui for the parser layer.
func Atoui(s string) uint32 { return atoui(s) }

// Atoi64 parses a leading signed decimal into int64. ok is false only on
// overflow (FX_SAFE_FILESIZE invalid); a non-numeric prefix yields (0, true).
func Atoi64(s string) (int64, bool) {
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	var n int64
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		d := int64(s[i] - '0')
		if n > (maxInt64-d)/10 {
			return 0, false
		}
		n = n*10 + d
		i++
	}
	if neg {
		n = -n
	}
	return n, true
}

const maxInt64 = int64(^uint64(0) >> 1)
