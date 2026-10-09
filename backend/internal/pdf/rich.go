package pdf

import (
	"fmt"
	"math"
	"strings"

	"kupol/internal/documents"
)

// rich — фрагменты текста читателя (OutRun) в разметке Typst.
//
// Закрытый фрагмент — чёрная полоса той же «живой» ширины, что и на сайте: она зависит от положения
// фрагмента (шифр, номер, видимый текст перед ним), но не от скрытого — длина закрытого не должна
// утекать. Функция и константы повторяют frontend/src/lib/realism.ts (redactionWidth).
const (
	redactionMin  = 3.5
	redactionMax  = 9.0
	redactionStep = 0.5
	monoAdvance   = 0.6 // ширина знака IBM Plex Mono в em (600/1000)
)

// redactionHash — FNV-1a, как hash() в realism.ts.
func redactionHash(seed string) uint32 {
	h := uint32(2166136261)
	for _, c := range seed {
		h ^= uint32(c)
		h *= 16777619
	}
	return h
}

// redactionWidth — ширина полосы в «ch» (3.5–9, шаг 0.5), как redactionWidth() в realism.ts.
func redactionWidth(seed string) float64 {
	steps := int(math.Round((redactionMax - redactionMin) / redactionStep))
	return redactionMin + float64(redactionHash(seed)%uint32(steps+1))*redactionStep
}

// redactionBox — чёрная полоса закрытого фрагмента: ширина от положения, высота как у сайта (1.05em).
func redactionBox(code string, idx int, before int, level int) string {
	seed := fmt.Sprintf("%s|%d|%d|%d", code, idx, before, level)
	width := redactionWidth(seed) * monoAdvance
	return fmt.Sprintf("#box(fill: black, width: %.2fem, height: 1.02em, baseline: -0.18em)", width)
}

// rich — фрагменты в разметку; code — шифр документа для ширины зачернений.
func rich(code string, runs []documents.OutRun) string {
	var b strings.Builder
	before := 0
	for i, run := range runs {
		if run.Redacted {
			b.WriteString(redactionBox(code, i, before, run.Level))
			before += len([]rune(run.Text))
			continue
		}
		text := escape(run.Text)
		switch {
		case run.Bold && run.Italic:
			b.WriteString("#strong[#emph[" + text + "]]")
		case run.Bold:
			b.WriteString("#strong[" + text + "]")
		case run.Italic:
			b.WriteString("#emph[" + text + "]")
		default:
			b.WriteString(text)
		}
		before += len([]rune(run.Text))
	}
	return b.String()
}

// plain — те же фрагменты одной строкой (для ячеек и подписей, где переносы не нужны).
func plain(code string, runs []documents.OutRun) string {
	return strings.ReplaceAll(rich(code, runs), "\\\n", " ")
}
