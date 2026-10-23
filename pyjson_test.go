package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// largestSamples returns the decompressed JSON of the n largest revisions in
// .data/notes_sample.jsonl (user data, not in the repo; see TestNotesSample).
func largestSamples(b *testing.B, n int) [][]byte {
	f, err := os.Open(".data/notes_sample.jsonl")
	if os.IsNotExist(err) {
		b.Skip("needs .data/notes_sample.jsonl")
	} else if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(nil, 4<<20)
	var all [][]byte
	for line := 1; scanner.Scan(); line++ {
		var r struct {
			ValueB64 string `json:"value_b64"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			b.Fatalf("record %d: %v", line, err)
		}
		blob, err := base64.StdEncoding.DecodeString(r.ValueB64)
		if err != nil {
			b.Fatalf("record %d: %v", line, err)
		}
		data, err := zlibDecompress(blob)
		if err != nil {
			b.Fatalf("record %d: %v", line, err)
		}
		all = append(all, data)
	}
	if err := scanner.Err(); err != nil {
		b.Fatal(err)
	}
	sort.Slice(all, func(i, j int) bool { return len(all[i]) > len(all[j]) })
	return all[:min(n, len(all))]
}

// Parsing must take linear time: a parser that copied the rest of the input
// for each token made large workspaces take 20-50 s to load (2026-10-23).
// Allocated bytes are a deterministic stand-in for time.
func TestParsePyJSONAllocatesLinearly(t *testing.T) {
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < 2000; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, `{"id": "note%d", "xPos": %d, "text": "café \"%d\"", "bgcolor": null, "visible": true}`, i, i, i)
	}
	b.WriteString("]")
	data := []byte(b.String())

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := parsePyJSON(data); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 50*uint64(len(data)) {
		t.Errorf("parsing %d bytes allocated %d bytes", len(data), alloc)
	}
}

// Parsing and re-dumping notes is what the workspace page and RSS feed do
// with every revision they show.
func BenchmarkParsePyJSONLargest(b *testing.B) {
	samples := largestSamples(b, 5)
	total := 0
	for _, s := range samples {
		total += len(s)
	}
	b.SetBytes(int64(total))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range samples {
			v, err := parsePyJSON(s)
			if err != nil {
				b.Fatal(err)
			}
			pyDumps(v)
		}
	}
}
