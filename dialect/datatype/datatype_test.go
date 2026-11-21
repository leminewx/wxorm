package datatype

import (
	"strconv"
	"sync"
	"testing"
)

// BenchmarkStringsFormatUint-2   	51861016	        22.13 ns/op	       3 B/op	       1 allocs/op
func BenchmarkStringsFormatUint(b *testing.B) {
	cache := &sync.Map{}
	for i := 0; i < 1000; i++ {
		cache.Store(i, strconv.FormatUint(uint64(i), 10))
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, ok := cache.Load(100); ok {
			continue
		}

		cache.Store(100, strconv.FormatUint(100, 10))
	}
}
