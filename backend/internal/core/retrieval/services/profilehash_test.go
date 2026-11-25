package services

import (
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func goldenProfile() iface.RetrievalIngestionProfile {
	return iface.RetrievalIngestionProfile{EmbedEndpoint: "embed",
		Analyzer: iface.RetrievalAnalyzer{Tokenizer: "word", Language: "english", Stemming: true, StopWords: true},
		Chunking: iface.RetrievalChunking{Strategy: "fixed", MaxTokens: 256, Overlap: 32}}
}

func TestIngestionProfileHash_Golden(t *testing.T) {
	assert.Equal(t, "ing_2081e190bb0f276d81cd2dfb97d27dab2db7c910dddd201a4f615e7588336ab7", IngestionProfileHash(goldenProfile(), "ip_test"))
}

func TestIngestionProfileHash_NormalizesDefaults(t *testing.T) {
	p := iface.RetrievalIngestionProfile{EmbedEndpoint: "other",
		Chunking: iface.RetrievalChunking{Strategy: "", MaxTokens: 99, Overlap: 3}}
	assert.Equal(t, "ing_2dcbe5e728fe270dc429a92aa72aca3ecc62698136dfcc2da75f31cde34bebac", IngestionProfileHash(p, "ip_test"))
}

func TestIngestionProfileHash_EveryTransformationFieldMatters(t *testing.T) {
	base := IngestionProfileHash(goldenProfile(), "ip_test")
	mut := func(f func(p *iface.RetrievalIngestionProfile)) string {
		p := goldenProfile()
		f(&p)
		return IngestionProfileHash(p, "ip_test")
	}
	assert.NotEqual(t, base, IngestionProfileHash(goldenProfile(), "ip_other"))
	for name, h := range map[string]string{
		"language":  mut(func(p *iface.RetrievalIngestionProfile) { p.Analyzer.Language = "italian" }),
		"stemming":  mut(func(p *iface.RetrievalIngestionProfile) { p.Analyzer.Stemming = false }),
		"stopWords": mut(func(p *iface.RetrievalIngestionProfile) { p.Analyzer.StopWords = false }),
		"tokenizer": mut(func(p *iface.RetrievalIngestionProfile) { p.Analyzer.Tokenizer = "multilingual" }),
		"strategy":  mut(func(p *iface.RetrievalIngestionProfile) { p.Chunking.Strategy = "none" }),
		"maxTokens": mut(func(p *iface.RetrievalIngestionProfile) { p.Chunking.MaxTokens = 128 }),
		"overlap":   mut(func(p *iface.RetrievalIngestionProfile) { p.Chunking.Overlap = 0 }),
		"enrichment": mut(func(p *iface.RetrievalIngestionProfile) {
			p.Enrichment = &iface.RetrievalEnrichment{ChatEndpoint: "chat", PromptVersion: "v1"}
		}),
	} {
		assert.NotEqual(t, base, h, name)
	}
	assert.Equal(t, base, mut(func(p *iface.RetrievalIngestionProfile) { p.EmbedEndpoint = "renamed" }))
}

func TestClampWorkerSettings(t *testing.T) {
	cases := []struct{ in, want WorkerSettings }{
		{WorkerSettings{}, WorkerSettings{2 * time.Minute, 32, 5 * time.Second}},
		{WorkerSettings{time.Second, 1000, 100 * time.Millisecond}, WorkerSettings{30 * time.Second, 256, time.Second}},
		{WorkerSettings{time.Hour, -3, time.Hour}, WorkerSettings{15 * time.Minute, 32, 5 * time.Minute}},
		{WorkerSettings{90 * time.Second, 7, 2 * time.Second}, WorkerSettings{90 * time.Second, 7, 2 * time.Second}},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, ClampWorkerSettings(c.in))
	}
}
