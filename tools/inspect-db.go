package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := `C:\Users\payne\AppData\Local\Paprika Recipe Manager 3\Database\Paprika.sqlite`

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Get all tables
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	fmt.Println("Tables:")
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			log.Fatal(err)
		}
		tables = append(tables, name)
		fmt.Printf("  - %s\n", name)
	}

	// Get schema for each table
	fmt.Println("\nSchemas:")
	for _, table := range tables {
		var schema string
		err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&schema)
		if err != nil {
			continue
		}
		fmt.Printf("\n%s:\n%s\n", table, schema)
	}

	// Query meal types
	fmt.Println("\n\nMeal Types:")
	typeRows, err := db.Query("SELECT uid, name, order_flag FROM meal_types ORDER BY order_flag")
	if err != nil {
		log.Printf("Error querying meal_types: %v", err)
	} else {
		defer typeRows.Close()
		for typeRows.Next() {
			var uid, name string
			var orderFlag int
			if err := typeRows.Scan(&uid, &name, &orderFlag); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("  %d: %s (UID: %s)\n", orderFlag, name, uid)
		}
	}
}
