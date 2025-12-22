package kvp

import (
	"context"
	"fmt"
	"testing"

	"github.com/RashikAnsar/raftkv/internal/storage"
)

// BenchmarkCommand_PING benchmarks PING command
func BenchmarkCommand_PING(b *testing.B) {
	cmd := &PingCommand{}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cmd.Execute(ctx, []string{})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCommand_GET benchmarks GET command
func BenchmarkCommand_GET(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	ctx := context.Background()
	store.Put(ctx, "testkey", []byte("testvalue"))

	cmd := &GetCommand{store: store}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cmd.Execute(ctx, []string{"testkey"})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCommand_SET benchmarks SET command
func BenchmarkCommand_SET(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	cmd := &SetCommand{store: store}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key%d", i)
		_, err := cmd.Execute(ctx, []string{key, "value"})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCommand_SET_WithTTL benchmarks SET command with TTL
func BenchmarkCommand_SET_WithTTL(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	cmd := &SetCommand{store: store}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key%d", i)
		_, err := cmd.Execute(ctx, []string{key, "value", "EX", "60"})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCommand_DEL benchmarks DEL command
func BenchmarkCommand_DEL(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	cmd := &DelCommand{store: store}
	ctx := context.Background()

	// Pre-populate keys
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("delkey%d", i)
		store.Put(ctx, key, []byte("value"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("delkey%d", i)
		_, err := cmd.Execute(ctx, []string{key})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCommand_EXISTS benchmarks EXISTS command
func BenchmarkCommand_EXISTS(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	ctx := context.Background()
	store.Put(ctx, "existkey", []byte("value"))

	cmd := &ExistsCommand{store: store}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cmd.Execute(ctx, []string{"existkey"})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCommand_LPUSH benchmarks LPUSH command
func BenchmarkCommand_LPUSH(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	cmd := &QueuePushCommand{store: store, side: "left"}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cmd.Execute(ctx, []string{"mylist", "value"})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCommand_LPOP benchmarks LPOP command
func BenchmarkCommand_LPOP(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	ctx := context.Background()
	cmd := &QueuePopCommand{store: store, side: "left"}

	// Pre-populate list with N items
	pushCmd := &QueuePushCommand{store: store, side: "left"}
	for i := 0; i < b.N; i++ {
		pushCmd.Execute(ctx, []string{"poplist", "value"})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cmd.Execute(ctx, []string{"poplist"})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCommand_KEYS benchmarks KEYS command
func BenchmarkCommand_KEYS(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	ctx := context.Background()

	// Pre-populate with 100 keys
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("user:%d", i)
		store.Put(ctx, key, []byte("value"))
	}

	cmd := &KeysCommand{store: store}

	b.Run("AllKeys", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := cmd.Execute(ctx, []string{"*"})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("PrefixMatch", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := cmd.Execute(ctx, []string{"user:*"})
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCommandRegistry_Execute benchmarks command execution through registry
func BenchmarkCommandRegistry_Execute(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	registry := NewCommandRegistry(store)
	ctx := context.Background()

	// Pre-populate a key
	store.Put(ctx, "benchkey", []byte("benchvalue"))

	b.Run("PING", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := registry.Execute(ctx, "PING", []string{})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("GET", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := registry.Execute(ctx, "GET", []string{"benchkey"})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("SET", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%d", i)
			_, err := registry.Execute(ctx, "SET", []string{key, "value"})
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCommand_Parallel benchmarks parallel command execution
func BenchmarkCommand_Parallel(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	registry := NewCommandRegistry(store)
	ctx := context.Background()

	b.Run("GET_Parallel", func(b *testing.B) {
		// Pre-populate keys
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("pkey%d", i)
			store.Put(ctx, key, []byte("value"))
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("pkey%d", i%1000)
				_, err := registry.Execute(ctx, "GET", []string{key})
				if err != nil {
					b.Fatal(err)
				}
				i++
			}
		})
	})

	b.Run("SET_Parallel", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("setkey%d", i)
				_, err := registry.Execute(ctx, "SET", []string{key, "value"})
				if err != nil {
					b.Fatal(err)
				}
				i++
			}
		})
	})

	b.Run("Mixed_Parallel", func(b *testing.B) {
		// Pre-populate keys
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("mixkey%d", i)
			store.Put(ctx, key, []byte("value"))
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("mixkey%d", i%1000)
				if i%2 == 0 {
					registry.Execute(ctx, "GET", []string{key})
				} else {
					registry.Execute(ctx, "SET", []string{key, "newvalue"})
				}
				i++
			}
		})
	})
}
