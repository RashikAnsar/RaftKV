package kvp

import (
	"context"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) storage.Store {
	store := storage.NewMemoryStore()
	return store
}

func TestPingCommand(t *testing.T) {
	cmd := &PingCommand{}

	tests := []struct {
		name     string
		args     []string
		expected string
		isError  bool
	}{
		{"no args", []string{}, "PONG", false},
		{"with message", []string{"hello"}, "hello", false},
		{"too many args", []string{"a", "b"}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := cmd.Execute(context.Background(), tt.args)
			if tt.isError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result.Str)
			}
		})
	}
}

func TestGetCommand(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Setup
	store.Put(ctx, "key1", []byte("value1"))

	cmd := &GetCommand{store}

	tests := []struct {
		name    string
		args    []string
		want    string
		isNull  bool
		isError bool
	}{
		{"existing key", []string{"key1"}, "value1", false, false},
		{"non-existing key", []string{"key2"}, "", true, false},
		{"no args", []string{}, "", false, true},
		{"too many args", []string{"a", "b"}, "", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := cmd.Execute(ctx, tt.args)
			if tt.isError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.isNull {
					assert.True(t, result.Null)
				} else {
					assert.Equal(t, tt.want, result.Str)
				}
			}
		})
	}
}

func TestSetCommand(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	cmd := &SetCommand{store}

	t.Run("basic set", func(t *testing.T) {
		result, err := cmd.Execute(ctx, []string{"key1", "value1"})
		require.NoError(t, err)
		assert.Equal(t, "OK", result.Str)

		// Verify
		value, err := store.Get(ctx, "key1")
		require.NoError(t, err)
		assert.Equal(t, "value1", string(value))
	})

	t.Run("set with EX", func(t *testing.T) {
		result, err := cmd.Execute(ctx, []string{"key2", "value2", "EX", "10"})
		require.NoError(t, err)
		assert.Equal(t, "OK", result.Str)

		// Verify TTL is set
		ttl, err := store.GetTTL(ctx, "key2")
		require.NoError(t, err)
		assert.Greater(t, ttl, time.Duration(0))
		assert.LessOrEqual(t, ttl, 10*time.Second)
	})

	t.Run("set NX - not exists", func(t *testing.T) {
		result, err := cmd.Execute(ctx, []string{"key-new", "value", "NX"})
		require.NoError(t, err)
		assert.Equal(t, "OK", result.Str)
	})

	t.Run("set NX - exists", func(t *testing.T) {
		store.Put(ctx, "existing", []byte("old"))
		result, err := cmd.Execute(ctx, []string{"existing", "new", "NX"})
		require.NoError(t, err)
		assert.True(t, result.Null)

		// Verify old value unchanged
		value, _ := store.Get(ctx, "existing")
		assert.Equal(t, "old", string(value))
	})

	t.Run("set XX - exists", func(t *testing.T) {
		store.Put(ctx, "existing2", []byte("old"))
		result, err := cmd.Execute(ctx, []string{"existing2", "new", "XX"})
		require.NoError(t, err)
		assert.Equal(t, "OK", result.Str)

		// Verify updated
		value, _ := store.Get(ctx, "existing2")
		assert.Equal(t, "new", string(value))
	})

	t.Run("set XX - not exists", func(t *testing.T) {
		result, err := cmd.Execute(ctx, []string{"not-exists", "value", "XX"})
		require.NoError(t, err)
		assert.True(t, result.Null)
	})

	t.Run("invalid args", func(t *testing.T) {
		_, err := cmd.Execute(ctx, []string{"key"})
		assert.Error(t, err)
	})
}

func TestDelCommand(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Setup
	store.Put(ctx, "key1", []byte("value1"))
	store.Put(ctx, "key2", []byte("value2"))

	cmd := &DelCommand{store}

	t.Run("delete single key", func(t *testing.T) {
		result, err := cmd.Execute(ctx, []string{"key1"})
		require.NoError(t, err)
		assert.Equal(t, int64(1), result.Int)

		// Verify deleted
		_, err = store.Get(ctx, "key1")
		assert.Error(t, err)
	})

	t.Run("delete multiple keys", func(t *testing.T) {
		store.Put(ctx, "a", []byte("1"))
		store.Put(ctx, "b", []byte("2"))
		store.Put(ctx, "c", []byte("3"))

		result, err := cmd.Execute(ctx, []string{"a", "b", "nonexistent"})
		require.NoError(t, err)
		assert.Equal(t, int64(2), result.Int)
	})

	t.Run("no args", func(t *testing.T) {
		_, err := cmd.Execute(ctx, []string{})
		assert.Error(t, err)
	})
}

func TestExistsCommand(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Setup
	store.Put(ctx, "key1", []byte("value1"))

	cmd := &ExistsCommand{store}

	tests := []struct {
		name     string
		args     []string
		expected int64
	}{
		{"exists", []string{"key1"}, 1},
		{"not exists", []string{"key2"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := cmd.Execute(ctx, tt.args)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.Int)
		})
	}
}

func TestExpireCommand(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Setup
	store.Put(ctx, "key1", []byte("value1"))

	cmd := &ExpireCommand{store}

	t.Run("set expire on existing key", func(t *testing.T) {
		result, err := cmd.Execute(ctx, []string{"key1", "60"})
		require.NoError(t, err)
		assert.Equal(t, int64(1), result.Int)

		// Verify TTL
		ttl, err := store.GetTTL(ctx, "key1")
		require.NoError(t, err)
		assert.Greater(t, ttl, time.Duration(0))
	})

	t.Run("set expire on non-existing key", func(t *testing.T) {
		result, err := cmd.Execute(ctx, []string{"nonexistent", "60"})
		require.NoError(t, err)
		assert.Equal(t, int64(0), result.Int)
	})

	t.Run("invalid seconds", func(t *testing.T) {
		_, err := cmd.Execute(ctx, []string{"key1", "invalid"})
		assert.Error(t, err)
	})
}

func TestTTLCommand(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	cmd := &TTLCommand{store}

	t.Run("key with TTL", func(t *testing.T) {
		store.PutWithTTL(ctx, "key1", []byte("value"), 60*time.Second)

		result, err := cmd.Execute(ctx, []string{"key1"})
		require.NoError(t, err)
		assert.Greater(t, result.Int, int64(0))
		assert.LessOrEqual(t, result.Int, int64(60))
	})

	t.Run("key without TTL", func(t *testing.T) {
		store.Put(ctx, "key2", []byte("value"))

		result, err := cmd.Execute(ctx, []string{"key2"})
		require.NoError(t, err)
		assert.Equal(t, int64(-1), result.Int)
	})

	t.Run("non-existing key", func(t *testing.T) {
		result, err := cmd.Execute(ctx, []string{"nonexistent"})
		require.NoError(t, err)
		assert.Equal(t, int64(-2), result.Int)
	})
}

func TestKeysCommand(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Setup
	store.Put(ctx, "user:1", []byte("alice"))
	store.Put(ctx, "user:2", []byte("bob"))
	store.Put(ctx, "session:1", []byte("abc"))

	cmd := &KeysCommand{store}

	t.Run("all keys", func(t *testing.T) {
		result, err := cmd.Execute(ctx, []string{"*"})
		require.NoError(t, err)
		assert.Equal(t, byte(TypeArray), result.Type)
		assert.Len(t, result.Array, 3)
	})

	t.Run("prefix pattern", func(t *testing.T) {
		result, err := cmd.Execute(ctx, []string{"user:*"})
		require.NoError(t, err)
		assert.Equal(t, byte(TypeArray), result.Type)
		assert.Len(t, result.Array, 2)
	})

	t.Run("exact match exists", func(t *testing.T) {
		result, err := cmd.Execute(ctx, []string{"user:1"})
		require.NoError(t, err)
		assert.Len(t, result.Array, 1)
		assert.Equal(t, "user:1", result.Array[0].Str)
	})

	t.Run("exact match not exists", func(t *testing.T) {
		result, err := cmd.Execute(ctx, []string{"nonexistent"})
		require.NoError(t, err)
		assert.Len(t, result.Array, 0)
	})
}

func TestQueueCommands(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	t.Run("LPUSH and LPOP", func(t *testing.T) {
		lpush := &QueuePushCommand{store, "left"}
		lpop := &QueuePopCommand{store, "left"}

		// Push
		result, err := lpush.Execute(ctx, []string{"mylist", "a", "b", "c"})
		require.NoError(t, err)
		assert.Equal(t, int64(3), result.Int)

		// Pop (should return c, b, a in order)
		result, err = lpop.Execute(ctx, []string{"mylist"})
		require.NoError(t, err)
		assert.Equal(t, "c", result.Str)

		result, err = lpop.Execute(ctx, []string{"mylist"})
		require.NoError(t, err)
		assert.Equal(t, "b", result.Str)

		result, err = lpop.Execute(ctx, []string{"mylist"})
		require.NoError(t, err)
		assert.Equal(t, "a", result.Str)

		// Pop from empty list
		result, err = lpop.Execute(ctx, []string{"mylist"})
		require.NoError(t, err)
		assert.True(t, result.Null)
	})

	t.Run("RPUSH and RPOP", func(t *testing.T) {
		rpush := &QueuePushCommand{store, "right"}
		rpop := &QueuePopCommand{store, "right"}

		// Push
		result, err := rpush.Execute(ctx, []string{"mylist2", "x", "y", "z"})
		require.NoError(t, err)
		assert.Equal(t, int64(3), result.Int)

		// Pop (should return z, y, x in order)
		result, err = rpop.Execute(ctx, []string{"mylist2"})
		require.NoError(t, err)
		assert.Equal(t, "z", result.Str)
	})
}

func TestCommandRegistry(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	registry := NewCommandRegistry(store)

	t.Run("execute PING", func(t *testing.T) {
		result, err := registry.Execute(ctx, "PING", []string{})
		require.NoError(t, err)
		assert.Equal(t, "PONG", result.Str)
	})

	t.Run("execute ping (lowercase)", func(t *testing.T) {
		result, err := registry.Execute(ctx, "ping", []string{})
		require.NoError(t, err)
		assert.Equal(t, "PONG", result.Str)
	})

	t.Run("unknown command", func(t *testing.T) {
		_, err := registry.Execute(ctx, "UNKNOWN", []string{})
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidCommand)
	})

	t.Run("execute SET and GET", func(t *testing.T) {
		// SET
		result, err := registry.Execute(ctx, "SET", []string{"testkey", "testvalue"})
		require.NoError(t, err)
		assert.Equal(t, "OK", result.Str)

		// GET
		result, err = registry.Execute(ctx, "GET", []string{"testkey"})
		require.NoError(t, err)
		assert.Equal(t, "testvalue", result.Str)
	})
}
