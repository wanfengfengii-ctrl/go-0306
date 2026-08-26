package retest

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// BlockTopology captures the deterministic neighborhood facts used to expand an
// anomaly into a retest member set: the mix pan, kiln-car position, shared wire
// wear window and raw batch of every candidate block.
type BlockTopology struct {
	Pan        map[string]string `json:"pan"`
	Position   map[string]int64  `json:"position"`
	WireWindow map[string]string `json:"wire_window"`
	RawBatch   map[string]string `json:"raw_batch"`
}

// AdjacentPositions returns the fixed adjacency table for a kiln-car position:
// the immediate predecessor and successor.
func AdjacentPositions(pos int64) []int64 {
	return []int64{pos - 1, pos + 1}
}

// Propagate computes the deterministic, ordered retest member set for a source
// block: the union of all blocks sharing the source's mix pan, raw batch, wire
// wear window, or an adjacent kiln-car position, plus the source itself. The
// result is sorted ascending so identical anomalies always serialize
// identically.
func Propagate(source string, topo BlockTopology, all []string) []string {
	set := map[string]bool{source: true}
	pan := topo.Pan[source]
	batch := topo.RawBatch[source]
	wire := topo.WireWindow[source]
	adj := AdjacentPositions(topo.Position[source])
	adjSet := make(map[int64]bool, len(adj))
	for _, a := range adj {
		adjSet[a] = true
	}
	for _, b := range all {
		if topo.Pan[b] == pan || topo.RawBatch[b] == batch || topo.WireWindow[b] == wire || adjSet[topo.Position[b]] {
			set[b] = true
		}
	}
	out := make([]string, 0, len(set))
	for b := range set {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

// RetestKey derives the stable, canonical unique key for a retest set from the
// normalized anomaly type, source evidence and ordered member list. Concurrent
// creators of the same key compare-and-set on it, so only one generation wins.
func RetestKey(anomaly Anomaly, source string, members []string) string {
	h := sha256.New()
	h.Write([]byte(anomaly))
	h.Write([]byte{0})
	h.Write([]byte(source))
	h.Write([]byte{0})
	h.Write([]byte(strings.Join(members, ",")))
	return hex.EncodeToString(h.Sum(nil))
}

// SortedStrings returns a stable copy of s in ascending order.
func SortedStrings(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}
