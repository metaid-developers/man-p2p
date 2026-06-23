package pebblestore

import (
	"io"

	"github.com/cockroachdb/pebble"
)

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return cloned
}

func closePebbleValue(closer io.Closer) {
	if closer != nil {
		_ = closer.Close()
	}
}

func getClonedValue(db *pebble.DB, key []byte) ([]byte, error) {
	value, closer, err := db.Get(key)
	if err != nil {
		return nil, err
	}
	cloned := cloneBytes(value)
	closePebbleValue(closer)
	return cloned, nil
}
