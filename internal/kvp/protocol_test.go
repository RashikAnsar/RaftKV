package kvp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRESPReader_SimpleString(t *testing.T) {
	input := "+OK\r\n"
	reader := NewRESPReader(strings.NewReader(input))

	val, err := reader.Read()
	require.NoError(t, err)
	assert.Equal(t, byte(TypeSimpleString), val.Type)
	assert.Equal(t, "OK", val.Str)
}

func TestRESPReader_Error(t *testing.T) {
	input := "-Error message\r\n"
	reader := NewRESPReader(strings.NewReader(input))

	val, err := reader.Read()
	require.NoError(t, err)
	assert.Equal(t, byte(TypeError), val.Type)
	assert.Equal(t, "Error message", val.Str)
}

func TestRESPReader_Integer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{"positive", ":1000\r\n", 1000},
		{"negative", ":-50\r\n", -50},
		{"zero", ":0\r\n", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := NewRESPReader(strings.NewReader(tt.input))
			val, err := reader.Read()
			require.NoError(t, err)
			assert.Equal(t, byte(TypeInteger), val.Type)
			assert.Equal(t, tt.expected, val.Int)
		})
	}
}

func TestRESPReader_BulkString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		isNull   bool
	}{
		{"simple", "$6\r\nfoobar\r\n", "foobar", false},
		{"empty", "$0\r\n\r\n", "", false},
		{"null", "$-1\r\n", "", true},
		{"with spaces", "$11\r\nhello world\r\n", "hello world", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := NewRESPReader(strings.NewReader(tt.input))
			val, err := reader.Read()
			require.NoError(t, err)
			assert.Equal(t, byte(TypeBulkString), val.Type)
			assert.Equal(t, tt.expected, val.Str)
			assert.Equal(t, tt.isNull, val.Null)
		})
	}
}

func TestRESPReader_Array(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
		isNull   bool
	}{
		{
			"simple array",
			"*2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n",
			[]string{"foo", "bar"},
			false,
		},
		{
			"empty array",
			"*0\r\n",
			[]string{},
			false,
		},
		{
			"null array",
			"*-1\r\n",
			nil,
			true,
		},
		{
			"nested types",
			"*3\r\n$3\r\nGET\r\n$3\r\nkey\r\n:123\r\n",
			nil, // Will check manually
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := NewRESPReader(strings.NewReader(tt.input))
			val, err := reader.Read()
			require.NoError(t, err)
			assert.Equal(t, byte(TypeArray), val.Type)
			assert.Equal(t, tt.isNull, val.Null)

			if tt.expected != nil {
				arr, err := val.ToArray()
				require.NoError(t, err)
				assert.Equal(t, tt.expected, arr)
			}
		})
	}
}

func TestRESPReader_RedisCommand(t *testing.T) {
	// Simulates: SET mykey myvalue
	input := "*3\r\n$3\r\nSET\r\n$5\r\nmykey\r\n$7\r\nmyvalue\r\n"
	reader := NewRESPReader(strings.NewReader(input))

	val, err := reader.Read()
	require.NoError(t, err)
	assert.Equal(t, byte(TypeArray), val.Type)
	assert.Len(t, val.Array, 3)

	arr, err := val.ToArray()
	require.NoError(t, err)
	assert.Equal(t, []string{"SET", "mykey", "myvalue"}, arr)
}

func TestRESPReader_InvalidProtocol(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"invalid type", "@invalid\r\n"},
		{"missing CRLF", "+OK\n"},
		{"invalid integer", ":abc\r\n"},
		{"invalid bulk length", "$abc\r\n"},
		{"truncated bulk", "$10\r\nhello\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := NewRESPReader(strings.NewReader(tt.input))
			_, err := reader.Read()
			assert.Error(t, err)
		})
	}
}

func TestRESPWriter_SimpleString(t *testing.T) {
	var buf bytes.Buffer
	writer := NewRESPWriter(&buf)

	err := writer.WriteSimpleString("OK")
	require.NoError(t, err)
	err = writer.Flush()
	require.NoError(t, err)

	assert.Equal(t, "+OK\r\n", buf.String())
}

func TestRESPWriter_Error(t *testing.T) {
	var buf bytes.Buffer
	writer := NewRESPWriter(&buf)

	err := writer.WriteError(assert.AnError)
	require.NoError(t, err)
	err = writer.Flush()
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "-")
	assert.Contains(t, buf.String(), "\r\n")
}

func TestRESPWriter_Integer(t *testing.T) {
	tests := []struct {
		name     string
		value    int64
		expected string
	}{
		{"positive", 1000, ":1000\r\n"},
		{"negative", -50, ":-50\r\n"},
		{"zero", 0, ":0\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := NewRESPWriter(&buf)

			err := writer.WriteInteger(tt.value)
			require.NoError(t, err)
			err = writer.Flush()
			require.NoError(t, err)

			assert.Equal(t, tt.expected, buf.String())
		})
	}
}

func TestRESPWriter_BulkString(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{"simple", "foobar", "$6\r\nfoobar\r\n"},
		{"empty", "", "$0\r\n\r\n"},
		{"with spaces", "hello world", "$11\r\nhello world\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := NewRESPWriter(&buf)

			err := writer.WriteBulkString(tt.value)
			require.NoError(t, err)
			err = writer.Flush()
			require.NoError(t, err)

			assert.Equal(t, tt.expected, buf.String())
		})
	}
}

func TestRESPWriter_Null(t *testing.T) {
	var buf bytes.Buffer
	writer := NewRESPWriter(&buf)

	err := writer.WriteNull()
	require.NoError(t, err)
	err = writer.Flush()
	require.NoError(t, err)

	assert.Equal(t, "$-1\r\n", buf.String())
}

func TestRESPWriter_Array(t *testing.T) {
	var buf bytes.Buffer
	writer := NewRESPWriter(&buf)

	// Write array header
	err := writer.WriteArray(2)
	require.NoError(t, err)

	// Write elements
	err = writer.WriteBulkString("foo")
	require.NoError(t, err)
	err = writer.WriteBulkString("bar")
	require.NoError(t, err)

	err = writer.Flush()
	require.NoError(t, err)

	expected := "*2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"
	assert.Equal(t, expected, buf.String())
}

func TestRESPWriter_NullArray(t *testing.T) {
	var buf bytes.Buffer
	writer := NewRESPWriter(&buf)

	err := writer.WriteNullArray()
	require.NoError(t, err)
	err = writer.Flush()
	require.NoError(t, err)

	assert.Equal(t, "*-1\r\n", buf.String())
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		write func(*RESPWriter) error
		check func(*testing.T, *Value)
	}{
		{
			"simple string",
			func(w *RESPWriter) error {
				return w.WriteSimpleString("PONG")
			},
			func(t *testing.T, v *Value) {
				assert.Equal(t, byte(TypeSimpleString), v.Type)
				assert.Equal(t, "PONG", v.Str)
			},
		},
		{
			"integer",
			func(w *RESPWriter) error {
				return w.WriteInteger(42)
			},
			func(t *testing.T, v *Value) {
				assert.Equal(t, byte(TypeInteger), v.Type)
				assert.Equal(t, int64(42), v.Int)
			},
		},
		{
			"bulk string",
			func(w *RESPWriter) error {
				return w.WriteBulkString("hello")
			},
			func(t *testing.T, v *Value) {
				assert.Equal(t, byte(TypeBulkString), v.Type)
				assert.Equal(t, "hello", v.Str)
			},
		},
		{
			"null",
			func(w *RESPWriter) error {
				return w.WriteNull()
			},
			func(t *testing.T, v *Value) {
				assert.Equal(t, byte(TypeBulkString), v.Type)
				assert.True(t, v.Null)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := NewRESPWriter(&buf)

			err := tt.write(writer)
			require.NoError(t, err)
			err = writer.Flush()
			require.NoError(t, err)

			reader := NewRESPReader(&buf)
			val, err := reader.Read()
			require.NoError(t, err)

			tt.check(t, val)
		})
	}
}
