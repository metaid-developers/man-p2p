package main

import (
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
)

func main() {
	db, err := pebble.Open("/srv/dev_project/metaid/man-indexer-v2/man_base_data_pebble/mrc20/db", &pebble.Options{})
	if err != nil {
		panic(err)
	}
	defer db.Close()

	utxos := []string{
		"19bfccfb9dda00a8c753d36baaddf2256dba8e5e6f494bfd5dba07d4d1035393:0",
		"19bfccfb9dda00a8c753d36baaddf2256dba8e5e6f494bfd5dba07d4d1035393:1",
		"331dbc48cbb8098caa71a98632451e77a0453ea61ce538746de5ea3975dd8ea7:0",
		"331dbc48cbb8098caa71a98632451e77a0453ea61ce538746de5ea3975dd8ea7:1",
	}

	fmt.Println("=== UTXO ===")
	for _, txp := range utxos {
		key := "mrc20_utxo_" + txp
		data, closer, err := db.Get([]byte(key))
		if err != nil {
			fmt.Printf("X %s: not found\n", txp)
			continue
		}
		var u map[string]interface{}
		sonic.Unmarshal(data, &u)
		closer.Close()
		fmt.Printf("O %s: amt=%v, status=%v, opt=%v\n", txp, u["amtChange"], u["status"], u["mrcOption"])
	}

	fmt.Println("\n=== Transaction ===")
	txKeys := []string{
		"tx_btc_19bfccfb9dda00a8c753d36baaddf2256dba8e5e6f494bfd5dba07d4d1035393:0",
		"tx_btc_19bfccfb9dda00a8c753d36baaddf2256dba8e5e6f494bfd5dba07d4d1035393:1",
		"tx_btc_331dbc48cbb8098caa71a98632451e77a0453ea61ce538746de5ea3975dd8ea7:0",
		"tx_btc_331dbc48cbb8098caa71a98632451e77a0453ea61ce538746de5ea3975dd8ea7:1",
	}
	for _, key := range txKeys {
		data, closer, err := db.Get([]byte(key))
		if err != nil {
			fmt.Printf("X %s: not found\n", key)
			continue
		}
		var tx map[string]interface{}
		sonic.Unmarshal(data, &tx)
		closer.Close()
		fmt.Printf("O %s: amt=%v, status=%v, type=%v\n", key, tx["amount"], tx["status"], tx["txType"])
	}
}
