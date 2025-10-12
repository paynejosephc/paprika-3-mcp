package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/soggycactus/paprika-3-mcp/internal/paprika"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	username := os.Getenv("PAPRIKA_USERNAME")
	password := os.Getenv("PAPRIKA_PASSWORD")

	if username == "" || password == "" {
		log.Fatal("PAPRIKA_USERNAME and PAPRIKA_PASSWORD must be set")
	}

	client, err := paprika.NewClient(username, password, "test", logger)
	if err != nil {
		log.Fatal(err)
	}

	// Test creating a menu item
	menuItem := paprika.MenuItem{
		RecipeUID: "TEST-RECIPE-UID",
		Name:      "Test Menu Item",
		Date:      "2025-10-05",
		TypeUID:   "216713D08860CFA0D9787EA5C6CEBC8A8F5B73777F91C904853AC234BB9DF642", // Dinner
		OrderFlag: 0,
	}

	// Print the menu item as JSON to see what's being sent
	data, _ := json.MarshalIndent(menuItem, "", "  ")
	fmt.Println("Menu item to create:")
	fmt.Println(string(data))

	savedMenuItem, err := client.SaveMenuItem(context.Background(), menuItem)
	if err != nil {
		log.Fatalf("Failed to create menu item: %v", err)
	}

	fmt.Println("\nCreated menu item:")
	data, _ = json.MarshalIndent(savedMenuItem, "", "  ")
	fmt.Println(string(data))
}
