package store

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
)

// BenchmarkVectorSearch 测试向量检索性能
func BenchmarkVectorSearch(b *testing.B) {
	sizes := []int{1000, 5000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			dbPath := fmt.Sprintf("bench_%d.db", size)
			defer os.Remove(dbPath)

			store, err := New(Config{DBPath: dbPath, VectorDim: 1024})
			if err != nil {
				b.Fatal(err)
			}
			defer store.Close()

			// 插入测试数据
			for i := 0; i < size; i++ {
				vec := make([]float32, 1024)
				for j := range vec {
					vec[j] = rand.Float32()
				}
				_, err := store.Insert(&Memory{
					Text:       fmt.Sprintf("memory %d", i),
					Vector:     vec,
					Category:   "fact",
					Scope:      "global",
					Importance: 0.5,
				})
				if err != nil {
					b.Fatal(err)
				}
			}

			// 准备查询向量
			query := make([]float32, 1024)
			for i := range query {
				query[i] = rand.Float32()
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := store.VectorSearch(query, 10, []string{"global"})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkParallelVectorSearch 测试并行向量检索性能
func BenchmarkParallelVectorSearch(b *testing.B) {
	sizes := []int{1000, 5000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			dbPath := fmt.Sprintf("bench_parallel_%d.db", size)
			defer os.Remove(dbPath)

			s, err := New(Config{DBPath: dbPath, VectorDim: 1024})
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()

			store := s.(*sqliteStore)

			// 插入测试数据
			for i := 0; i < size; i++ {
				vec := make([]float32, 1024)
				for j := range vec {
					vec[j] = rand.Float32()
				}
				_, err := store.Insert(&Memory{
					Text:       fmt.Sprintf("memory %d", i),
					Vector:     vec,
					Category:   "fact",
					Scope:      "global",
					Importance: 0.5,
				})
				if err != nil {
					b.Fatal(err)
				}
			}

			// 准备查询向量
			query := make([]float32, 1024)
			for i := range query {
				query[i] = rand.Float32()
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := store.parallelVectorSearch(query, 10, []string{"global"})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
