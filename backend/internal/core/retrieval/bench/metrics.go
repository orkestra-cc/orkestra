package bench

import (
	"math"
	"sort"
	"time"
)

// RecallAtK is the fraction of relevant ids found among the first k ranked
// ids. It returns 0 when relevant is empty or k <= 0.
func RecallAtK(ranked, relevant []string, k int) float64 {
	if len(relevant) == 0 || k <= 0 {
		return 0
	}
	rel := idSet(relevant)
	seen := make(map[string]struct{}, len(rel))
	for _, id := range topK(ranked, k) {
		if _, ok := rel[id]; ok {
			seen[id] = struct{}{}
		}
	}
	return float64(len(seen)) / float64(len(rel))
}

// NDCGAtK is the normalised discounted cumulative gain at k with binary
// relevance: DCG = sum(rel_i / log2(i+1)) for rank i from 1, divided by the
// DCG of an ideal ranking. It returns 0 when relevant is empty or k <= 0.
func NDCGAtK(ranked, relevant []string, k int) float64 {
	if len(relevant) == 0 || k <= 0 {
		return 0
	}
	rel := idSet(relevant)
	var dcg float64
	counted := make(map[string]struct{}, len(rel))
	for i, id := range topK(ranked, k) {
		if _, ok := rel[id]; !ok {
			continue
		}
		if _, dup := counted[id]; dup {
			continue
		}
		counted[id] = struct{}{}
		dcg += 1 / math.Log2(float64(i+2))
	}
	var ideal float64
	for i := 0; i < min(len(rel), k); i++ {
		ideal += 1 / math.Log2(float64(i+2))
	}
	return dcg / ideal
}

// P95 is the nearest-rank 95th percentile, sorted[ceil(0.95*n)-1]. It
// returns 0 for an empty slice and leaves d unchanged.
func P95(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[int(math.Ceil(0.95*float64(len(s))))-1]
}

func topK(ranked []string, k int) []string {
	if len(ranked) > k {
		return ranked[:k]
	}
	return ranked
}

func idSet(ids []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}
