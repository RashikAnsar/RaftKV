package kvp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/RashikAnsar/raftkv/internal/storage"
)

var (
	ErrWrongNumberArgs = errors.New("wrong number of arguments")
	ErrInvalidCommand  = errors.New("unknown command")
	ErrInvalidInteger  = errors.New("value is not an integer or out of range")
	ErrInvalidExpire   = errors.New("invalid expire time")
)

// CommandHandler handles a Redis command
type CommandHandler interface {
	Execute(ctx context.Context, args []string) (*Value, error)
}

// CommandRegistry maps command names to handlers
type CommandRegistry struct {
	store    storage.Store
	handlers map[string]CommandHandler
}

// NewCommandRegistry creates a new command registry
func NewCommandRegistry(store storage.Store) *CommandRegistry {
	r := &CommandRegistry{
		store:    store,
		handlers: make(map[string]CommandHandler),
	}

	// Register commands
	r.register("PING", &PingCommand{})
	r.register("GET", &GetCommand{store})
	r.register("SET", &SetCommand{store})
	r.register("DEL", &DelCommand{store})
	r.register("EXISTS", &ExistsCommand{store})
	r.register("EXPIRE", &ExpireCommand{store})
	r.register("TTL", &TTLCommand{store})
	r.register("KEYS", &KeysCommand{store})
	r.register("LPUSH", &QueuePushCommand{store, "left"})
	r.register("RPUSH", &QueuePushCommand{store, "right"})
	r.register("LPOP", &QueuePopCommand{store, "left"})
	r.register("RPOP", &QueuePopCommand{store, "right"})

	return r
}

// register adds a command handler
func (r *CommandRegistry) register(name string, handler CommandHandler) {
	r.handlers[strings.ToUpper(name)] = handler
}

// Execute executes a command
func (r *CommandRegistry) Execute(ctx context.Context, cmdName string, args []string) (*Value, error) {
	handler, ok := r.handlers[strings.ToUpper(cmdName)]
	if !ok {
		return nil, fmt.Errorf("%w: '%s'", ErrInvalidCommand, cmdName)
	}

	return handler.Execute(ctx, args)
}

// PingCommand implements PING
type PingCommand struct{}

func (c *PingCommand) Execute(ctx context.Context, args []string) (*Value, error) {
	if len(args) == 0 {
		return &Value{Type: TypeSimpleString, Str: "PONG"}, nil
	}
	if len(args) == 1 {
		return &Value{Type: TypeBulkString, Str: args[0]}, nil
	}
	return nil, ErrWrongNumberArgs
}

// GetCommand implements GET key
type GetCommand struct {
	store storage.Store
}

func (c *GetCommand) Execute(ctx context.Context, args []string) (*Value, error) {
	if len(args) != 1 {
		return nil, ErrWrongNumberArgs
	}

	value, err := c.store.Get(ctx, args[0])
	if err != nil {
		if errors.Is(err, storage.ErrKeyNotFound) {
			return &Value{Type: TypeBulkString, Null: true}, nil
		}
		return nil, err
	}

	return &Value{Type: TypeBulkString, Str: string(value)}, nil
}

// SetCommand implements SET key value [EX seconds] [NX|XX]
type SetCommand struct {
	store storage.Store
}

func (c *SetCommand) Execute(ctx context.Context, args []string) (*Value, error) {
	if len(args) < 2 {
		return nil, ErrWrongNumberArgs
	}

	key := args[0]
	value := []byte(args[1])
	var ttl time.Duration
	nx := false // Only set if key doesn't exist
	xx := false // Only set if key exists

	// Parse options
	for i := 2; i < len(args); i++ {
		opt := strings.ToUpper(args[i])
		switch opt {
		case "EX":
			if i+1 >= len(args) {
				return nil, ErrWrongNumberArgs
			}
			seconds, err := strconv.Atoi(args[i+1])
			if err != nil || seconds <= 0 {
				return nil, ErrInvalidInteger
			}
			ttl = time.Duration(seconds) * time.Second
			i++
		case "NX":
			nx = true
		case "XX":
			xx = true
		default:
			return nil, fmt.Errorf("unsupported option: %s", opt)
		}
	}

	// Check NX/XX conditions
	if nx || xx {
		_, err := c.store.Get(ctx, key)
		exists := err == nil

		if nx && exists {
			// NX: Only set if not exists
			return &Value{Type: TypeBulkString, Null: true}, nil
		}
		if xx && !exists {
			// XX: Only set if exists
			return &Value{Type: TypeBulkString, Null: true}, nil
		}
	}

	// Execute SET
	var err error
	if ttl > 0 {
		err = c.store.PutWithTTL(ctx, key, value, ttl)
	} else {
		err = c.store.Put(ctx, key, value)
	}

	if err != nil {
		return nil, err
	}

	return &Value{Type: TypeSimpleString, Str: "OK"}, nil
}

// DelCommand implements DEL key [key ...]
type DelCommand struct {
	store storage.Store
}

func (c *DelCommand) Execute(ctx context.Context, args []string) (*Value, error) {
	if len(args) < 1 {
		return nil, ErrWrongNumberArgs
	}

	deleted := int64(0)
	for _, key := range args {
		// Check if key exists first (Redis DEL only counts existing keys)
		_, err := c.store.Get(ctx, key)
		if err == nil {
			// Key exists, delete it
			if delErr := c.store.Delete(ctx, key); delErr == nil {
				deleted++
			}
		}
	}

	return &Value{Type: TypeInteger, Int: deleted}, nil
}

// ExistsCommand implements EXISTS key
type ExistsCommand struct {
	store storage.Store
}

func (c *ExistsCommand) Execute(ctx context.Context, args []string) (*Value, error) {
	if len(args) != 1 {
		return nil, ErrWrongNumberArgs
	}

	_, err := c.store.Get(ctx, args[0])
	if err != nil {
		if errors.Is(err, storage.ErrKeyNotFound) {
			return &Value{Type: TypeInteger, Int: 0}, nil
		}
		return nil, err
	}

	return &Value{Type: TypeInteger, Int: 1}, nil
}

// ExpireCommand implements EXPIRE key seconds
type ExpireCommand struct {
	store storage.Store
}

func (c *ExpireCommand) Execute(ctx context.Context, args []string) (*Value, error) {
	if len(args) != 2 {
		return nil, ErrWrongNumberArgs
	}

	key := args[0]
	seconds, err := strconv.Atoi(args[1])
	if err != nil || seconds < 0 {
		return nil, ErrInvalidInteger
	}

	// Check if key exists
	_, err = c.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrKeyNotFound) {
			return &Value{Type: TypeInteger, Int: 0}, nil
		}
		return nil, err
	}

	// Set TTL
	ttl := time.Duration(seconds) * time.Second
	err = c.store.SetTTL(ctx, key, ttl)
	if err != nil {
		return nil, err
	}

	return &Value{Type: TypeInteger, Int: 1}, nil
}

// TTLCommand implements TTL key
type TTLCommand struct {
	store storage.Store
}

func (c *TTLCommand) Execute(ctx context.Context, args []string) (*Value, error) {
	if len(args) != 1 {
		return nil, ErrWrongNumberArgs
	}

	key := args[0]

	// Check if key exists
	_, err := c.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrKeyNotFound) {
			return &Value{Type: TypeInteger, Int: -2}, nil // Key doesn't exist
		}
		return nil, err
	}

	// Get TTL
	ttl, err := c.store.GetTTL(ctx, key)
	if err != nil {
		return nil, err
	}

	// If TTL is 0, it means no expiration is set
	if ttl == 0 {
		return &Value{Type: TypeInteger, Int: -1}, nil // No TTL set
	}

	seconds := int64(ttl.Seconds())
	return &Value{Type: TypeInteger, Int: seconds}, nil
}

// KeysCommand implements KEYS pattern
type KeysCommand struct {
	store storage.Store
}

func (c *KeysCommand) Execute(ctx context.Context, args []string) (*Value, error) {
	if len(args) != 1 {
		return nil, ErrWrongNumberArgs
	}

	pattern := args[0]

	// Convert Redis pattern to prefix (simplified - only supports * and prefix:*)
	var prefix string
	if pattern == "*" {
		prefix = ""
	} else if strings.HasSuffix(pattern, "*") {
		prefix = strings.TrimSuffix(pattern, "*")
	} else {
		// Exact match
		_, err := c.store.Get(ctx, pattern)
		if err != nil {
			if errors.Is(err, storage.ErrKeyNotFound) {
				return &Value{Type: TypeArray, Array: []Value{}}, nil
			}
			return nil, err
		}
		return &Value{
			Type: TypeArray,
			Array: []Value{
				{Type: TypeBulkString, Str: pattern},
			},
		}, nil
	}

	// List keys with prefix
	result, err := c.store.ListWithOptions(ctx, storage.ListOptions{
		Prefix: prefix,
		Limit:  1000, // Limit to prevent huge responses
	})
	if err != nil {
		return nil, err
	}

	// Convert to array
	array := make([]Value, len(result.Keys))
	for i, key := range result.Keys {
		array[i] = Value{Type: TypeBulkString, Str: key}
	}

	return &Value{Type: TypeArray, Array: array}, nil
}

// QueuePushCommand implements LPUSH/RPUSH key value [value ...]
type QueuePushCommand struct {
	store storage.Store
	side  string // "left" or "right"
}

func (c *QueuePushCommand) Execute(ctx context.Context, args []string) (*Value, error) {
	if len(args) < 2 {
		return nil, ErrWrongNumberArgs
	}

	key := args[0]

	// Get existing list or create new one
	var list []string
	value, err := c.store.Get(ctx, key)
	if err == nil {
		// Parse existing list (stored as JSON array or newline-separated)
		list = strings.Split(string(value), "\n")
		if len(list) == 1 && list[0] == "" {
			list = []string{}
		}
	} else if !errors.Is(err, storage.ErrKeyNotFound) {
		return nil, err
	}

	// Add values
	for _, val := range args[1:] {
		if c.side == "left" {
			list = append([]string{val}, list...)
		} else {
			list = append(list, val)
		}
	}

	// Store updated list
	newValue := strings.Join(list, "\n")
	err = c.store.Put(ctx, key, []byte(newValue))
	if err != nil {
		return nil, err
	}

	return &Value{Type: TypeInteger, Int: int64(len(list))}, nil
}

// QueuePopCommand implements LPOP/RPOP key
type QueuePopCommand struct {
	store storage.Store
	side  string // "left" or "right"
}

func (c *QueuePopCommand) Execute(ctx context.Context, args []string) (*Value, error) {
	if len(args) != 1 {
		return nil, ErrWrongNumberArgs
	}

	key := args[0]

	// Get existing list
	value, err := c.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrKeyNotFound) {
			return &Value{Type: TypeBulkString, Null: true}, nil
		}
		return nil, err
	}

	// Parse list
	list := strings.Split(string(value), "\n")
	if len(list) == 0 || (len(list) == 1 && list[0] == "") {
		return &Value{Type: TypeBulkString, Null: true}, nil
	}

	// Pop value
	var popped string
	if c.side == "left" {
		popped = list[0]
		list = list[1:]
	} else {
		popped = list[len(list)-1]
		list = list[:len(list)-1]
	}

	// Update or delete
	if len(list) == 0 {
		err = c.store.Delete(ctx, key)
	} else {
		newValue := strings.Join(list, "\n")
		err = c.store.Put(ctx, key, []byte(newValue))
	}

	if err != nil {
		return nil, err
	}

	return &Value{Type: TypeBulkString, Str: popped}, nil
}
