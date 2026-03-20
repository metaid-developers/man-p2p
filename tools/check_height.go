package main

import (
	"fmt"
	"log"

	"github.com/cockroachdb/pebble"
)

func main() {
	db, err := pebble.Open("./man_base_data_pebble/meta/db", &pebble.Options{})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	keys := []string{
		"btc_mrc20_sync_height",
		"doge_mrc20_sync_height",
		"btc_sync_height",
		"doge_sync_height",
	}

	for _, key := range keys {
		value, closer, err := db.Get([]byte(key))
		if err != nil {
			if err == pebble.ErrNotFound {
				fmt.Printf("%-25s: NOT SET\n", key)
			} else {
				fmt.Printf("%-25s: ERROR - %v\n", key, err)
			}
		} else {
			fmt.Printf("%-25s: %s\n", key, string(value))
			closer.Close()
		}
	}
}
