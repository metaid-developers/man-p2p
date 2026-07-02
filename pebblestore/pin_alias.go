package pebblestore

import (
	"strings"

	"github.com/cockroachdb/pebble"
)

func (db *Database) BatchSetPinAliases(aliasPairs map[string]string) error {
	if db == nil || db.PinAliasDb == nil || len(aliasPairs) == 0 {
		return nil
	}
	batch := db.PinAliasDb.NewBatch()
	for alias, canonical := range aliasPairs {
		alias = strings.TrimSpace(alias)
		canonical = strings.TrimSpace(canonical)
		if alias == "" || canonical == "" || alias == canonical {
			continue
		}
		batch.Set([]byte(alias), []byte(canonical), nil)
	}
	if err := batch.Commit(nil); err != nil {
		batch.Close()
		return err
	}
	batch.Close()
	return nil
}

func (db *Database) SetPinAlias(alias, canonical string) error {
	return db.BatchSetPinAliases(map[string]string{alias: canonical})
}

func (db *Database) GetPinAlias(alias string) (string, error) {
	if db == nil || db.PinAliasDb == nil {
		return "", pebble.ErrNotFound
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return "", pebble.ErrNotFound
	}
	val, closer, err := db.PinAliasDb.Get([]byte(alias))
	if err != nil {
		return "", err
	}
	defer closer.Close()
	return string(val), nil
}
