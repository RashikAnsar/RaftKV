package kvp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// RESP (REdis Serialization Protocol) implementation
// Specification: https://redis.io/docs/reference/protocol-spec/

// RESP data types
const (
	TypeSimpleString = '+' // +OK\r\n
	TypeError        = '-' // -Error message\r\n
	TypeInteger      = ':' // :1000\r\n
	TypeBulkString   = '$' // $6\r\nfoobar\r\n
	TypeArray        = '*' // *2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n
	TypeNull         = '_' // _\r\n (RESP3)
)

var (
	ErrInvalidProtocol = errors.New("invalid RESP protocol")
	ErrInvalidType     = errors.New("invalid RESP type")
	ErrInvalidLength   = errors.New("invalid length")
	ErrUnexpectedEOF   = errors.New("unexpected EOF")
)

// Value represents a RESP value
type Value struct {
	Type  byte
	Str   string
	Int   int64
	Array []Value
	Null  bool
}

// RESPReader reads RESP protocol messages
type RESPReader struct {
	reader *bufio.Reader
}

// NewRESPReader creates a new RESP reader
func NewRESPReader(r io.Reader) *RESPReader {
	return &RESPReader{
		reader: bufio.NewReader(r),
	}
}

// Read reads a RESP value
func (r *RESPReader) Read() (*Value, error) {
	typeByte, err := r.reader.ReadByte()
	if err != nil {
		return nil, err
	}

	switch typeByte {
	case TypeSimpleString:
		return r.readSimpleString()
	case TypeError:
		return r.readError()
	case TypeInteger:
		return r.readInteger()
	case TypeBulkString:
		return r.readBulkString()
	case TypeArray:
		return r.readArray()
	case TypeNull:
		return &Value{Type: TypeNull, Null: true}, nil
	default:
		return nil, fmt.Errorf("%w: unknown type '%c'", ErrInvalidType, typeByte)
	}
}

// readSimpleString reads a simple string (+OK\r\n)
func (r *RESPReader) readSimpleString() (*Value, error) {
	line, err := r.readLine()
	if err != nil {
		return nil, err
	}
	return &Value{Type: TypeSimpleString, Str: string(line)}, nil
}

// readError reads an error (-Error message\r\n)
func (r *RESPReader) readError() (*Value, error) {
	line, err := r.readLine()
	if err != nil {
		return nil, err
	}
	return &Value{Type: TypeError, Str: string(line)}, nil
}

// readInteger reads an integer (:1000\r\n)
func (r *RESPReader) readInteger() (*Value, error) {
	line, err := r.readLine()
	if err != nil {
		return nil, err
	}

	num, err := strconv.ParseInt(string(line), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid integer", ErrInvalidProtocol)
	}

	return &Value{Type: TypeInteger, Int: num}, nil
}

// readBulkString reads a bulk string ($6\r\nfoobar\r\n)
func (r *RESPReader) readBulkString() (*Value, error) {
	// Read length
	line, err := r.readLine()
	if err != nil {
		return nil, err
	}

	length, err := strconv.Atoi(string(line))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid bulk string length", ErrInvalidProtocol)
	}

	// Null bulk string
	if length == -1 {
		return &Value{Type: TypeBulkString, Null: true}, nil
	}

	if length < 0 {
		return nil, fmt.Errorf("%w: negative bulk string length", ErrInvalidLength)
	}

	// Read data
	data := make([]byte, length)
	_, err = io.ReadFull(r.reader, data)
	if err != nil {
		return nil, err
	}

	// Read trailing \r\n
	if err := r.readCRLF(); err != nil {
		return nil, err
	}

	return &Value{Type: TypeBulkString, Str: string(data)}, nil
}

// readArray reads an array (*2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n)
func (r *RESPReader) readArray() (*Value, error) {
	// Read count
	line, err := r.readLine()
	if err != nil {
		return nil, err
	}

	count, err := strconv.Atoi(string(line))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid array count", ErrInvalidProtocol)
	}

	// Null array
	if count == -1 {
		return &Value{Type: TypeArray, Null: true}, nil
	}

	if count < 0 {
		return nil, fmt.Errorf("%w: negative array count", ErrInvalidLength)
	}

	// Read elements
	array := make([]Value, count)
	for i := 0; i < count; i++ {
		val, err := r.Read()
		if err != nil {
			return nil, err
		}
		array[i] = *val
	}

	return &Value{Type: TypeArray, Array: array}, nil
}

// readLine reads a line until \r\n
func (r *RESPReader) readLine() ([]byte, error) {
	line, err := r.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	// Check for \r\n ending
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return nil, fmt.Errorf("%w: expected CRLF", ErrInvalidProtocol)
	}

	// Remove \r\n
	return line[:len(line)-2], nil
}

// readCRLF reads and validates \r\n
func (r *RESPReader) readCRLF() error {
	cr, err := r.reader.ReadByte()
	if err != nil {
		return err
	}
	if cr != '\r' {
		return fmt.Errorf("%w: expected CR", ErrInvalidProtocol)
	}

	lf, err := r.reader.ReadByte()
	if err != nil {
		return err
	}
	if lf != '\n' {
		return fmt.Errorf("%w: expected LF", ErrInvalidProtocol)
	}

	return nil
}

// RESPWriter writes RESP protocol messages
type RESPWriter struct {
	writer *bufio.Writer
}

// NewRESPWriter creates a new RESP writer
func NewRESPWriter(w io.Writer) *RESPWriter {
	return &RESPWriter{
		writer: bufio.NewWriter(w),
	}
}

// WriteSimpleString writes a simple string (+OK\r\n)
func (w *RESPWriter) WriteSimpleString(s string) error {
	if err := w.writer.WriteByte(TypeSimpleString); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(s); err != nil {
		return err
	}
	return w.writeCRLF()
}

// WriteError writes an error (-ERR message\r\n)
func (w *RESPWriter) WriteError(err error) error {
	if writeErr := w.writer.WriteByte(TypeError); writeErr != nil {
		return writeErr
	}
	if _, writeErr := w.writer.WriteString(err.Error()); writeErr != nil {
		return writeErr
	}
	return w.writeCRLF()
}

// WriteInteger writes an integer (:1000\r\n)
func (w *RESPWriter) WriteInteger(i int64) error {
	if err := w.writer.WriteByte(TypeInteger); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(strconv.FormatInt(i, 10)); err != nil {
		return err
	}
	return w.writeCRLF()
}

// WriteBulkString writes a bulk string ($6\r\nfoobar\r\n)
func (w *RESPWriter) WriteBulkString(s string) error {
	if err := w.writer.WriteByte(TypeBulkString); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(strconv.Itoa(len(s))); err != nil {
		return err
	}
	if err := w.writeCRLF(); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(s); err != nil {
		return err
	}
	return w.writeCRLF()
}

// WriteNull writes a null bulk string ($-1\r\n)
func (w *RESPWriter) WriteNull() error {
	if err := w.writer.WriteByte(TypeBulkString); err != nil {
		return err
	}
	if _, err := w.writer.WriteString("-1"); err != nil {
		return err
	}
	return w.writeCRLF()
}

// WriteArray writes an array (*2\r\n...)
func (w *RESPWriter) WriteArray(count int) error {
	if err := w.writer.WriteByte(TypeArray); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(strconv.Itoa(count)); err != nil {
		return err
	}
	return w.writeCRLF()
}

// WriteNullArray writes a null array (*-1\r\n)
func (w *RESPWriter) WriteNullArray() error {
	if err := w.writer.WriteByte(TypeArray); err != nil {
		return err
	}
	if _, err := w.writer.WriteString("-1"); err != nil {
		return err
	}
	return w.writeCRLF()
}

// Flush flushes the writer
func (w *RESPWriter) Flush() error {
	return w.writer.Flush()
}

// writeCRLF writes \r\n
func (w *RESPWriter) writeCRLF() error {
	if err := w.writer.WriteByte('\r'); err != nil {
		return err
	}
	return w.writer.WriteByte('\n')
}

// Helper functions for parsing commands

// ToArray converts a Value to an array of strings
func (v *Value) ToArray() ([]string, error) {
	if v.Type != TypeArray {
		return nil, fmt.Errorf("expected array, got %c", v.Type)
	}

	if v.Null {
		return nil, nil
	}

	result := make([]string, len(v.Array))
	for i, elem := range v.Array {
		if elem.Type == TypeBulkString {
			result[i] = elem.Str
		} else if elem.Type == TypeSimpleString {
			result[i] = elem.Str
		} else {
			return nil, fmt.Errorf("expected string in array, got %c", elem.Type)
		}
	}

	return result, nil
}

// String returns a string representation of the value
func (v *Value) String() string {
	var buf bytes.Buffer

	switch v.Type {
	case TypeSimpleString:
		buf.WriteByte(TypeSimpleString)
		buf.WriteString(v.Str)
	case TypeError:
		buf.WriteByte(TypeError)
		buf.WriteString(v.Str)
	case TypeInteger:
		buf.WriteByte(TypeInteger)
		buf.WriteString(strconv.FormatInt(v.Int, 10))
	case TypeBulkString:
		if v.Null {
			buf.WriteString("$-1")
		} else {
			buf.WriteString(fmt.Sprintf("$%d\r\n%s", len(v.Str), v.Str))
		}
	case TypeArray:
		if v.Null {
			buf.WriteString("*-1")
		} else {
			buf.WriteString(fmt.Sprintf("*%d", len(v.Array)))
			for _, elem := range v.Array {
				buf.WriteString("\r\n")
				buf.WriteString(elem.String())
			}
		}
	}

	return buf.String()
}
