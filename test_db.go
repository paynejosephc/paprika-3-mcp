package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	_ "modernc.org/sqlite"
)

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error getting home dir:", err)
		os.Exit(1)
	}
	
	dbPath := filepath.Join(homeDir, ".paprika-3-mcp", "recipes.db")
	fmt.Println("DB Path:", dbPath)
	
	// Try to create directory
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Println("Error creating directory:", err)
		os.Exit(1)
	}
	
	// Try to open database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Println("Error opening database:", err)
		os.Exit(1)
	}
	defer db.Close()
	
	// Try to ping
	if err := db.Ping(); err != nil {
		fmt.Println("Error pinging database:", err)
		os.Exit(1)
	}
	
	fmt.Println("Database opened successfully!")
}
