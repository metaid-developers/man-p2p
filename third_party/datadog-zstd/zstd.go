package zstd

import (
	"errors"
	"io"

	kzstd "github.com/klauspost/compress/zstd"
)

var errWriterClosed = errors.New("zstd: writer closed")

func CompressBound(srcSize int) int {
	if srcSize <= 0 {
		return 64
	}
	// Conservative upper bound used only for pre-allocation in callers.
	return srcSize + (srcSize / 8) + 256
}

func Compress(dst, src []byte) ([]byte, error) {
	return CompressLevel(dst, src, 3)
}

func CompressLevel(dst, src []byte, level int) ([]byte, error) {
	encoder, err := kzstd.NewWriter(nil, kzstd.WithEncoderLevel(kzstd.EncoderLevelFromZstd(level)))
	if err != nil {
		return nil, err
	}
	defer encoder.Close()
	return encoder.EncodeAll(src, dst[:0]), nil
}

func Decompress(dst, src []byte) ([]byte, error) {
	decoder, err := kzstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	return decoder.DecodeAll(src, dst[:0])
}

func DecompressInto(dst, src []byte) (int, error) {
	decoded, err := Decompress(dst, src)
	if err != nil {
		return 0, err
	}
	if len(decoded) > len(dst) {
		return 0, errors.New("zstd: destination too small")
	}
	return copy(dst, decoded), nil
}

type Writer struct {
	out    io.Writer
	enc    *kzstd.Encoder
	closed bool
}

func NewWriter(w io.Writer) *Writer {
	return NewWriterLevel(w, 3)
}

func NewWriterLevel(w io.Writer, level int) *Writer {
	enc, err := kzstd.NewWriter(nil, kzstd.WithEncoderLevel(kzstd.EncoderLevelFromZstd(level)))
	if err != nil {
		enc, _ = kzstd.NewWriter(nil)
	}
	return &Writer{
		out: w,
		enc: enc,
	}
}

func (w *Writer) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errWriterClosed
	}
	if len(p) == 0 {
		return 0, nil
	}
	if w.out == nil {
		return len(p), nil
	}
	compressed := w.enc.EncodeAll(p, nil)
	if _, err := w.out.Write(compressed); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.enc != nil {
		w.enc.Close()
	}
	return nil
}
