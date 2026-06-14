package tui

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type screen struct {
	w, h   int
	x, y   int
	sx, sy int
	rows   [][]rune
	esc    []rune
	inEsc  bool
}

func newScreen(w, h int) *screen {
	s := &screen{}
	s.resize(w, h)
	return s
}

func (s *screen) resize(w, h int) {
	w, h = max(20, w), max(5, h)
	old := s.rows
	s.w, s.h = w, h
	s.rows = make([][]rune, h)
	for y := range s.rows {
		s.rows[y] = make([]rune, w)
		for x := range s.rows[y] {
			s.rows[y][x] = ' '
		}
	}
	for y := 0; y < len(old) && y < h; y++ {
		copy(s.rows[y], old[y])
	}
	s.x = clamp(s.x, 0, w-1)
	s.y = clamp(s.y, 0, h-1)
}

func (s *screen) write(text string) {
	for _, r := range text {
		s.put(r)
	}
}

func (s *screen) put(r rune) {
	if s.inEsc {
		s.esc = append(s.esc, r)
		if s.escDone() {
			s.applyEsc(string(s.esc))
			s.esc = nil
			s.inEsc = false
		}
		return
	}
	switch r {
	case '\x1b':
		s.inEsc = true
	case '\r':
		s.x = 0
	case '\n':
		s.nl()
	case '\b', 0x7f:
		if s.x > 0 {
			s.x--
		}
	case '\t':
		for target := ((s.x / 8) + 1) * 8; s.x < target; {
			s.print(' ')
		}
	default:
		if r == utf8.RuneError || r < 32 {
			return
		}
		s.print(r)
	}
}

func (s *screen) print(r rune) {
	if s.x >= s.w {
		s.nl()
	}
	s.rows[s.y][s.x] = r
	s.x++
	if s.x >= s.w {
		s.x = s.w - 1
	}
}

func (s *screen) nl() {
	s.x = 0
	s.y++
	if s.y < s.h {
		return
	}
	copy(s.rows, s.rows[1:])
	s.rows[s.h-1] = make([]rune, s.w)
	for x := range s.rows[s.h-1] {
		s.rows[s.h-1][x] = ' '
	}
	s.y = s.h - 1
}

func (s *screen) lines(limit int) []string {
	lines := make([]string, 0, len(s.rows))
	for _, row := range s.rows {
		lines = append(lines, strings.TrimRight(string(row), " "))
	}
	for len(lines) > 1 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	if len(lines) <= limit {
		return lines
	}
	return lines[len(lines)-limit:]
}

func (s *screen) escDone() bool {
	if len(s.esc) == 0 {
		return false
	}
	switch s.esc[0] {
	case '[':
		if len(s.esc) < 2 {
			return false
		}
		last := s.esc[len(s.esc)-1]
		return last >= '@' && last <= '~'
	case ']':
		return s.esc[len(s.esc)-1] == '\a' || (len(s.esc) > 1 && s.esc[len(s.esc)-2] == '\x1b' && s.esc[len(s.esc)-1] == '\\')
	case '(', ')':
		return len(s.esc) >= 2
	default:
		return true
	}
}

func (s *screen) applyEsc(seq string) {
	if seq == "" {
		return
	}
	switch seq[0] {
	case '[':
		s.csi(seq[1:])
	case 'c':
		s.clear()
	case '7':
		s.sx, s.sy = s.x, s.y
	case '8':
		s.x, s.y = clamp(s.sx, 0, s.w-1), clamp(s.sy, 0, s.h-1)
	}
}

func (s *screen) csi(seq string) {
	if seq == "" {
		return
	}
	final := seq[len(seq)-1]
	raw := strings.TrimPrefix(seq[:len(seq)-1], "?")
	params := params(raw)
	switch final {
	case 'A':
		s.y = clamp(s.y-param(params, 0, 1), 0, s.h-1)
	case 'B':
		s.y = clamp(s.y+param(params, 0, 1), 0, s.h-1)
	case 'C':
		s.x = clamp(s.x+param(params, 0, 1), 0, s.w-1)
	case 'D':
		s.x = clamp(s.x-param(params, 0, 1), 0, s.w-1)
	case 'G':
		s.x = clamp(param(params, 0, 1)-1, 0, s.w-1)
	case 'H', 'f':
		s.y = clamp(param(params, 0, 1)-1, 0, s.h-1)
		s.x = clamp(param(params, 1, 1)-1, 0, s.w-1)
	case 'J':
		if param(params, 0, 0) >= 2 {
			s.clear()
		}
	case 'K':
		for x := s.x; x < s.w; x++ {
			s.rows[s.y][x] = ' '
		}
	case 'h', 'l':
		if strings.Contains(raw, "1049") {
			s.clear()
		}
	case 's':
		s.sx, s.sy = s.x, s.y
	case 'u':
		s.x, s.y = clamp(s.sx, 0, s.w-1), clamp(s.sy, 0, s.h-1)
	}
}

func (s *screen) clear() {
	for y := range s.rows {
		for x := range s.rows[y] {
			s.rows[y][x] = ' '
		}
	}
	s.x, s.y = 0, 0
}

func params(raw string) []int {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ";")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		value, _ := strconv.Atoi(part)
		out = append(out, value)
	}
	return out
}

func param(values []int, idx, fallback int) int {
	if idx >= len(values) || values[idx] == 0 {
		return fallback
	}
	return values[idx]
}
