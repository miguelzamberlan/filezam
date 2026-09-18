// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package media

import (
	"bytes"
	"math/rand"
	"testing"
)

func vid(w, h int, dur int64, codec string, fps int, size int64) *Meta {
	return &Meta{Kind: KindVideo, Format: "mp4", Width: w, Height: h, DurationMS: dur, Codec: codec, FPSMilli: fps}
}

func img(w, h int, camera string) *Meta {
	return &Meta{Kind: KindImage, Format: "jpeg", Width: w, Height: h, Camera: camera}
}

func find(bs []Bucket, key string) *Bucket {
	for i := range bs {
		if bs[i].Key == key {
			return &bs[i]
		}
	}
	return nil
}

func TestAggregatorCountsAndBuckets(t *testing.T) {
	a := NewAggregator()
	a.Add(Sample{Path: "a/v1.mp4", Name: "v1.mp4", Size: 100, Media: true, Meta: vid(3840, 2160, 60_000, "H.265", 29970, 100)})
	a.Add(Sample{Path: "a/v2.mp4", Name: "v2.mp4", Size: 200, Media: true, Meta: vid(1920, 1080, 30_000, "H.264", 30000, 200)})
	a.Add(Sample{Path: "a/v3.mov", Name: "v3.mov", Size: 300, Media: true, Meta: vid(1080, 1920, 10_000, "H.264", 30000, 300)})
	a.Add(Sample{Path: "a/p1.jpg", Name: "p1.jpg", Size: 10, Media: true, Meta: img(6000, 4000, "NIKON Z 6")})
	a.Add(Sample{Path: "a/p2.jpg", Name: "p2.jpg", Size: 20, Media: true, Meta: img(3000, 4000, "NIKON Z 6")})
	a.Add(Sample{Path: "a/leiame.txt", Name: "leiame.txt", Size: 5})
	a.Add(Sample{Path: "a/quebrado.mp4", Name: "quebrado.mp4", Size: 7, Media: true})
	a.AddDir()

	st := a.Stats()
	if st.Files != 7 || st.Dirs != 1 || st.Bytes != 642 {
		t.Fatalf("totais: %+v", st)
	}
	if st.Videos != 3 || st.Images != 2 || st.Others != 1 || st.Unreadable != 1 {
		t.Fatalf("contagens: v=%d i=%d o=%d u=%d", st.Videos, st.Images, st.Others, st.Unreadable)
	}
	if st.VideoDuration != 100_000 {
		t.Fatalf("duração total: %d", st.VideoDuration)
	}
	if b := find(st.VideoRes, Res4K); b == nil || b.Count != 1 {
		t.Fatalf("faixa 4k: %+v", st.VideoRes)
	}
	// O vídeo em pé de 1080×1920 entra na mesma faixa do deitado de 1920×1080.
	if b := find(st.VideoRes, Res1080); b == nil || b.Count != 2 {
		t.Fatalf("faixa 1080p: %+v", st.VideoRes)
	}
	if b := find(st.VideoShape, Portrait); b == nil || b.Count != 1 {
		t.Fatalf("retrato: %+v", st.VideoShape)
	}
	if b := find(st.Codecs, "H.264"); b == nil || b.Count != 2 {
		t.Fatalf("codecs: %+v", st.Codecs)
	}
	if b := find(st.Cameras, "NIKON Z 6"); b == nil || b.Count != 2 {
		t.Fatalf("câmeras: %+v", st.Cameras)
	}
	if b := find(st.ImageShape, Portrait); b == nil || b.Count != 1 {
		t.Fatalf("retrato nas fotos: %+v", st.ImageShape)
	}
	if b := find(st.ImageMP, "mp24"); b == nil || b.Count != 1 {
		t.Fatalf("faixa de megapixels: %+v", st.ImageMP)
	}
	if st.Longest == nil || st.Longest.Name != "v1.mp4" {
		t.Fatalf("mais longo: %+v", st.Longest)
	}
	if st.Largest == nil || st.Largest.Name != "v3.mov" {
		t.Fatalf("maior: %+v", st.Largest)
	}
	if st.Sharpest == nil || st.Sharpest.Name != "p1.jpg" {
		t.Fatalf("mais pixels: %+v", st.Sharpest)
	}
	if st.Pixels != 6000*4000+3000*4000 {
		t.Fatalf("pixels: %d", st.Pixels)
	}
}

func TestFPSKeySnapsToStandardRates(t *testing.T) {
	cases := map[int]string{
		29969: "29.97", 29971: "29.97", 30000: "30", 23976: "23.976",
		59941: "59.94", 25000: "25", 0: "", 14500: "14.5",
	}
	for in, want := range cases {
		if got := FPSKey(in); got != want {
			t.Fatalf("FPSKey(%d) = %q, queria %q", in, got, want)
		}
	}
}

func TestResolutionKeyEdges(t *testing.T) {
	cases := []struct {
		w, h int
		key  string
	}{
		{7680, 4320, Res8K}, {4096, 2160, Res4K}, {3840, 2160, Res4K},
		{2560, 1440, Res2K}, {2560, 1080, Res1080}, {1920, 1080, Res1080},
		{1280, 720, Res720}, {640, 480, ResSD}, {320, 240, ResLow}, {0, 0, ResUnknown},
	}
	for _, c := range cases {
		if got := ResolutionKey(c.w, c.h); got != c.key {
			t.Fatalf("%dx%d = %q, queria %q", c.w, c.h, got, c.key)
		}
	}
}

func TestAggregatorCapsDistinctKeys(t *testing.T) {
	a := NewAggregator()
	for i := 0; i < maxKeys+500; i++ {
		a.Add(Sample{Path: "x", Name: "x", Size: 1, Media: true, Meta: &Meta{
			Kind: KindImage, Format: "jpeg", Width: 100 + i, Height: 100, Camera: "c" + string(rune('a'+i%26)),
		}})
	}
	st := a.Stats()
	if len(st.ImageRes) > topN {
		t.Fatalf("detalhamento aberto sem teto: %d", len(st.ImageRes))
	}
}

// TestProbeSurvivesGarbage joga bytes aleatórios em cada leitor: nenhum pode entrar em
// pânico nem devolver dimensões absurdas.
func TestProbeSurvivesGarbage(t *testing.T) {
	names := []string{"x.jpg", "x.png", "x.tiff", "x.cr2", "x.mp4", "x.mov", "x.heic", "x.mkv", "x.webm", "x.avi", "x.wav"}
	rnd := rand.New(rand.NewSource(7))
	for i := 0; i < 400; i++ {
		b := make([]byte, 1+rnd.Intn(4096))
		rnd.Read(b)
		name := names[i%len(names)]
		m, err := Probe(bytes.NewReader(b), name, int64(len(b)))
		if err != nil {
			continue
		}
		if m.Width < 0 || m.Height < 0 || m.DurationMS < 0 {
			t.Fatalf("%s: valores negativos em %+v", name, m)
		}
	}
}
