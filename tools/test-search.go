package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := `C:\Users\payne\AppData\Local\Paprika Recipe Manager 3\Database\Paprika.sqlite`

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Test the exact query from SearchRecipes
	query := `
	SELECT uid, name, ingredients, directions, description, notes,
		   servings, prep_time, cook_time, difficulty,
		   rating, in_trash, created
	FROM recipes
	WHERE in_trash = 0
	LIMIT 2
	`

	rows, err := db.Query(query)
	if err != nil {
		log.Fatal("Query error:", err)
	}
	defer rows.Close()

	for rows.Next() {
		var uid, name, ingredients, directions, description, notes string
		var servings, prepTime, cookTime, difficulty string
		var rating, inTrash int
		var created interface{}

		err := rows.Scan(&uid, &name, &ingredients, &directions, &description, &notes,
			&servings, &prepTime, &cookTime, &difficulty,
			&rating, &inTrash, &created)
		if err != nil {
			log.Fatal("Scan error:", err)
		}

		fmt.Printf("UID: %s\n", uid)
		fmt.Printf("Name: %s\n", name)
		fmt.Printf("Rating: %d\n", rating)
		fmt.Printf("Created: %v (type: %T)\n", created, created)
		fmt.Println("---")
	}

	if err = rows.Err(); err != nil {
		log.Fatal("Rows error:", err)
	}
}
