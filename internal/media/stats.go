// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package media

import (
	"sort"
	"strconv"
)

// Chaves das faixas. São estáveis e vão para a interface como identificador: quem traduz
// é o frontend (web/src/i18n), do mesmo jeito que as notificações.
const (
	ResUnknown = "unknown"
	Res8K      = "8k"
	Res4K      = "4k"
	Res2K      = "2k"
	Res1080    = "1080p"
	Res720     = "720p"
	ResSD      = "sd"
	ResLow     = "low"
)

// resOrder fixa a ordem de exibição das faixas de resolução, da maior para a menor.
var resOrder = []string{Res8K, Res4K, Res2K, Res1080, Res720, ResSD, ResLow, ResUnknown}

// mpOrder fixa a ordem das faixas de megapixels.
var mpOrder = []string{"mp50", "mp24", "mp12", "mp8", "mp2", "mp1", ResUnknown}

var shapeOrder = []string{Landscape, Portrait, Square, ResUnknown}

// ResolutionKey classifies a picture by its shorter side, que é o lado que dá nome à
// faixa: 1920×1080 e 1080×1920 são os dois 1080p, um deitado e o outro em pé.
func ResolutionKey(w, h int) string {
	if w <= 0 || h <= 0 {
		return ResUnknown
	}
	short := min(w, h)
	switch {
	case short >= 4320:
		return Res8K
	case short >= 2160:
		return Res4K
	case short >= 1440:
		return Res2K
	case short >= 1080:
		return Res1080
	case short >= 720:
		return Res720
	case short >= 480:
		return ResSD
	}
	return ResLow
}

// megapixelKey classifies a still by how many megapixels it has.
func megapixelKey(w, h int) string {
	if w <= 0 || h <= 0 {
		return ResUnknown
	}
	mp := float64(w) * float64(h) / 1e6
	switch {
	case mp >= 50:
		return "mp50"
	case mp >= 24:
		return "mp24"
	case mp >= 12:
		return "mp12"
	case mp >= 8:
		return "mp8"
	case mp >= 2:
		return "mp2"
	}
	return "mp1"
}

// commonRates são as taxas que os equipamentos realmente usam. Encaixar nelas evita que
// 29,969 e 29,971, que são o mesmo ajuste de câmera, apareçam como duas linhas.
// fpsSnap é a distância máxima, em milésimos de quadro, para encaixar numa taxa padrão.
const fpsSnap = 20

var commonRates = []int{23976, 24000, 25000, 29970, 30000, 48000, 50000, 59940, 60000, 100000, 119880, 120000, 240000}

// FPSKey names a frame rate for grouping, snapping to the nearest standard rate.
func FPSKey(fpsMilli int) string {
	if fpsMilli <= 0 {
		return ""
	}
	// Encaixe pelo mais próximo, não pelo primeiro que serve: 29,97 e 30 estão a três
	// centésimos um do outro, e uma tolerância larga jogaria os dois na mesma linha.
	best, dist := 0, fpsSnap+1
	for _, r := range commonRates {
		d := fpsMilli - r
		if d < 0 {
			d = -d
		}
		if d < dist {
			best, dist = r, d
		}
	}
	if best > 0 {
		fpsMilli = best
	}
	if fpsMilli%1000 == 0 {
		return strconv.Itoa(fpsMilli / 1000)
	}
	s := strconv.FormatFloat(float64(fpsMilli)/1000, 'f', 3, 64)
	for len(s) > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	return s
}

// SizeKey is the exact "3840×2160" label used to group stills.
func SizeKey(w, h int) string {
	if w <= 0 || h <= 0 {
		return ResUnknown
	}
	return strconv.Itoa(w) + "×" + strconv.Itoa(h)
}

// Bucket is one line of a breakdown.
type Bucket struct {
	Key        string `json:"key"`
	Count      int    `json:"count"`
	Bytes      int64  `json:"bytes,omitempty"`
	DurationMS int64  `json:"durationMs,omitempty"`
}

// Item names one file in a superlative.
type Item struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Bytes      int64  `json:"bytes,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	DurationMS int64  `json:"durationMs,omitempty"`
}

// Stats is the whole analysis of a set of files.
type Stats struct {
	Files int   `json:"files"`
	Dirs  int   `json:"dirs"`
	Bytes int64 `json:"bytes"`

	Images int `json:"images"`
	Videos int `json:"videos"`
	Audios int `json:"audios"`
	// Others são arquivos que não são mídia que este servidor saiba ler; Unreadable são os
	// que prometiam mídia pela extensão e cujo cabeçalho não abriu.
	Others     int `json:"others"`
	Unreadable int `json:"unreadable"`

	ImageBytes int64 `json:"imageBytes"`
	VideoBytes int64 `json:"videoBytes"`
	AudioBytes int64 `json:"audioBytes"`
	OtherBytes int64 `json:"otherBytes"`

	VideoDuration int64 `json:"videoDuration"`
	AudioDuration int64 `json:"audioDuration"`
	Pixels        int64 `json:"pixels"`

	VideoRes   []Bucket `json:"videoRes"`
	ImageRes   []Bucket `json:"imageRes"`
	ImageMP    []Bucket `json:"imageMp"`
	ImageShape []Bucket `json:"imageShape"`
	VideoShape []Bucket `json:"videoShape"`
	Formats    []Bucket `json:"formats"`
	Codecs     []Bucket `json:"codecs"`
	FPS        []Bucket `json:"fps"`
	Cameras    []Bucket `json:"cameras"`

	Longest  *Item `json:"longest,omitempty"`
	Largest  *Item `json:"largest,omitempty"`
	Sharpest *Item `json:"sharpest,omitempty"`

	Oldest int64 `json:"oldest,omitempty"`
	Newest int64 `json:"newest,omitempty"`

	// Partial: a varredura parou num limite e o que está aqui é só parte da pasta.
	Partial bool `json:"partial"`
}

// maxKeys limita quantas chaves distintas cada detalhamento guarda. Nome de câmera e
// codec vêm do arquivo, então uma pasta hostil poderia inventar uma chave por arquivo.
const maxKeys = 4096

// topN é quanto de cada detalhamento aberto (formato, codec, câmera, tamanho exato) vai
// para a resposta; o resto é cauda longa que ninguém lê.
const topN = 24

// Sample is one file offered to the aggregator.
type Sample struct {
	Path string
	Name string
	Size int64
	// Meta é o que o leitor conseguiu; nil quando o cabeçalho não abriu.
	Meta *Meta
	// Media diz se a extensão prometia mídia, que é o que separa "não é mídia" de
	// "deveria ser mídia e não deu para ler".
	Media bool
}

// Aggregator turns samples into Stats.
type Aggregator struct {
	st Stats

	videoRes   map[string]*Bucket
	imageRes   map[string]*Bucket
	imageMP    map[string]*Bucket
	imageShape map[string]*Bucket
	videoShape map[string]*Bucket
	formats    map[string]*Bucket
	codecs     map[string]*Bucket
	fps        map[string]*Bucket
	cameras    map[string]*Bucket
}

func NewAggregator() *Aggregator {
	return &Aggregator{
		videoRes:   map[string]*Bucket{},
		imageRes:   map[string]*Bucket{},
		imageMP:    map[string]*Bucket{},
		imageShape: map[string]*Bucket{},
		videoShape: map[string]*Bucket{},
		formats:    map[string]*Bucket{},
		codecs:     map[string]*Bucket{},
		fps:        map[string]*Bucket{},
		cameras:    map[string]*Bucket{},
	}
}

// AddDir counts a folder visited during the walk.
func (a *Aggregator) AddDir() { a.st.Dirs++ }

// SetPartial marks the analysis as incomplete.
func (a *Aggregator) SetPartial() { a.st.Partial = true }

func add(m map[string]*Bucket, key string, size, dur int64) {
	if key == "" {
		return
	}
	b := m[key]
	if b == nil {
		if len(m) >= maxKeys {
			return
		}
		b = &Bucket{Key: key}
		m[key] = b
	}
	b.Count++
	b.Bytes += size
	b.DurationMS += dur
}

// Add folds one file into the totals.
func (a *Aggregator) Add(s Sample) {
	a.st.Files++
	a.st.Bytes += s.Size
	m := s.Meta
	if m == nil {
		if s.Media {
			a.st.Unreadable++
		} else {
			a.st.Others++
		}
		a.st.OtherBytes += s.Size
		return
	}
	add(a.formats, m.Format, s.Size, m.DurationMS)
	if m.Camera != "" {
		add(a.cameras, m.Camera, s.Size, m.DurationMS)
	}
	if m.TakenAt > 0 {
		if a.st.Oldest == 0 || m.TakenAt < a.st.Oldest {
			a.st.Oldest = m.TakenAt
		}
		if m.TakenAt > a.st.Newest {
			a.st.Newest = m.TakenAt
		}
	}
	if a.st.Largest == nil || s.Size > a.st.Largest.Bytes {
		a.st.Largest = itemOf(s, m)
	}

	switch m.Kind {
	case KindVideo:
		a.st.Videos++
		a.st.VideoBytes += s.Size
		a.st.VideoDuration += m.DurationMS
		add(a.videoRes, ResolutionKey(m.Width, m.Height), s.Size, m.DurationMS)
		add(a.videoShape, orientationKey(m), s.Size, m.DurationMS)
		add(a.codecs, m.Codec, s.Size, m.DurationMS)
		add(a.fps, FPSKey(m.FPSMilli), s.Size, m.DurationMS)
		if a.st.Longest == nil || m.DurationMS > a.st.Longest.DurationMS {
			a.st.Longest = itemOf(s, m)
		}
		a.sharpest(s, m)
	case KindImage:
		a.st.Images++
		a.st.ImageBytes += s.Size
		a.st.Pixels += m.Pixels()
		add(a.imageRes, SizeKey(m.Width, m.Height), s.Size, 0)
		add(a.imageMP, megapixelKey(m.Width, m.Height), s.Size, 0)
		add(a.imageShape, orientationKey(m), s.Size, 0)
		a.sharpest(s, m)
	case KindAudio:
		a.st.Audios++
		a.st.AudioBytes += s.Size
		a.st.AudioDuration += m.DurationMS
		add(a.codecs, m.AudioCodec, s.Size, m.DurationMS)
	default:
		a.st.Others++
		a.st.OtherBytes += s.Size
	}
}

func (a *Aggregator) sharpest(s Sample, m *Meta) {
	if m.Pixels() <= 0 {
		return
	}
	if a.st.Sharpest == nil || m.Pixels() > int64(a.st.Sharpest.Width)*int64(a.st.Sharpest.Height) {
		a.st.Sharpest = itemOf(s, m)
	}
}

func itemOf(s Sample, m *Meta) *Item {
	it := &Item{Path: s.Path, Name: s.Name, Bytes: s.Size}
	if m != nil {
		it.Width, it.Height, it.DurationMS = m.Width, m.Height, m.DurationMS
	}
	return it
}

func orientationKey(m *Meta) string {
	if o := m.Orientation(); o != "" {
		return o
	}
	return ResUnknown
}

// Stats closes the aggregation and returns the ordered breakdowns.
func (a *Aggregator) Stats() Stats {
	st := a.st
	st.VideoRes = ordered(a.videoRes, resOrder)
	st.ImageMP = ordered(a.imageMP, mpOrder)
	st.ImageShape = ordered(a.imageShape, shapeOrder)
	st.VideoShape = ordered(a.videoShape, shapeOrder)
	st.ImageRes = byCount(a.imageRes)
	st.Formats = byCount(a.formats)
	st.Codecs = byCount(a.codecs)
	st.Cameras = byCount(a.cameras)
	st.FPS = byFPS(a.fps)
	return st
}

// ordered returns the buckets in the fixed order, dropping the empty ones.
func ordered(m map[string]*Bucket, order []string) []Bucket {
	out := make([]Bucket, 0, len(order))
	for _, k := range order {
		if b := m[k]; b != nil && b.Count > 0 {
			out = append(out, *b)
		}
	}
	return out
}

// byCount returns the busiest buckets first, keeping ties in a stable order.
func byCount(m map[string]*Bucket) []Bucket {
	out := make([]Bucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}

// byFPS orders frame rates by the rate itself, not by how often it appears.
func byFPS(m map[string]*Bucket) []Bucket {
	out := make([]Bucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		vi, _ := strconv.ParseFloat(out[i].Key, 64)
		vj, _ := strconv.ParseFloat(out[j].Key, 64)
		if vi != vj {
			return vi < vj
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}
