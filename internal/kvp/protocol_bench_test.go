package kvp

import (
	"bytes"
	"testing"
)

// BenchmarkRESPWriter_SimpleString benchmarks writing simple strings
func BenchmarkRESPWriter_SimpleString(b *testing.B) {
	buf := &bytes.Buffer{}
	writer := NewRESPWriter(buf)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		writer.WriteSimpleString("OK")
	}
}

// BenchmarkRESPWriter_BulkString benchmarks writing bulk strings
func BenchmarkRESPWriter_BulkString(b *testing.B) {
	buf := &bytes.Buffer{}
	writer := NewRESPWriter(buf)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		writer.WriteBulkString("Hello, World! This is a test value.")
	}
}

// BenchmarkRESPWriter_Integer benchmarks writing integers
func BenchmarkRESPWriter_Integer(b *testing.B) {
	buf := &bytes.Buffer{}
	writer := NewRESPWriter(buf)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		writer.WriteInteger(12345)
	}
}

// BenchmarkRESPWriter_Array benchmarks writing arrays
func BenchmarkRESPWriter_Array(b *testing.B) {
	buf := &bytes.Buffer{}
	writer := NewRESPWriter(buf)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		writer.WriteArray(2)
		writer.WriteBulkString("GET")
		writer.WriteBulkString("mykey")
	}
}

// BenchmarkRESPReader_SimpleString benchmarks reading simple strings
func BenchmarkRESPReader_SimpleString(b *testing.B) {
	data := "+OK\r\n"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := bytes.NewBufferString(data)
		reader := NewRESPReader(buf)
		_, err := reader.Read()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRESPReader_BulkString benchmarks reading bulk strings
func BenchmarkRESPReader_BulkString(b *testing.B) {
	data := "$5\r\nhello\r\n"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := bytes.NewBufferString(data)
		reader := NewRESPReader(buf)
		_, err := reader.Read()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRESPReader_Integer benchmarks reading integers
func BenchmarkRESPReader_Integer(b *testing.B) {
	data := ":1000\r\n"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := bytes.NewBufferString(data)
		reader := NewRESPReader(buf)
		_, err := reader.Read()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRESPReader_Array benchmarks reading arrays
func BenchmarkRESPReader_Array(b *testing.B) {
	data := "*2\r\n$3\r\nGET\r\n$5\r\nmykey\r\n"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := bytes.NewBufferString(data)
		reader := NewRESPReader(buf)
		_, err := reader.Read()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRESPReader_LargeArray benchmarks reading large arrays
func BenchmarkRESPReader_LargeArray(b *testing.B) {
	// Array with 100 elements
	buf := bytes.NewBufferString("*100\r\n")
	for i := 0; i < 100; i++ {
		buf.WriteString("$5\r\nvalue\r\n")
	}
	data := buf.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := bytes.NewBufferString(data)
		reader := NewRESPReader(buf)
		_, err := reader.Read()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRESP_RoundTrip benchmarks full encode/decode cycle
func BenchmarkRESP_RoundTrip(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Write
		buf := &bytes.Buffer{}
		writer := NewRESPWriter(buf)
		writer.WriteArray(3)
		writer.WriteBulkString("SET")
		writer.WriteBulkString("mykey")
		writer.WriteBulkString("myvalue")
		writer.Flush()

		// Read
		reader := NewRESPReader(buf)
		_, err := reader.Read()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRESP_ParallelRead benchmarks concurrent reading
func BenchmarkRESP_ParallelRead(b *testing.B) {
	data := "*3\r\n$3\r\nSET\r\n$5\r\nmykey\r\n$7\r\nmyvalue\r\n"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := bytes.NewBufferString(data)
			reader := NewRESPReader(buf)
			_, err := reader.Read()
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkRESP_ParallelWrite benchmarks concurrent writing
func BenchmarkRESP_ParallelWrite(b *testing.B) {
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		buf := &bytes.Buffer{}
		writer := NewRESPWriter(buf)
		for pb.Next() {
			buf.Reset()
			writer.WriteSimpleString("OK")
		}
	})
}
