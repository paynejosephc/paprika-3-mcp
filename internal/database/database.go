package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"github.com/soggycactus/paprika-3-mcp/internal/paprika"
)

type RecipeDB struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewRecipeDB(dbPath string, logger *slog.Logger) (*RecipeDB, error) {
	// Open the existing Paprika database in read-only mode
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	rdb := &RecipeDB{
		db:     db,
		logger: logger,
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return rdb, nil
}

func (r *RecipeDB) SearchRecipes(searchQuery string, searchIn string, minRating, limit, offset int) ([]*paprika.Recipe, int, error) {
	// Build query with search filtering using Paprika's schema
	countQuery := "SELECT COUNT(*) FROM recipes WHERE in_trash = 0"
	query := `
	SELECT uid, name, ingredients, directions, description, notes,
		   servings, prep_time, cook_time, difficulty,
		   rating, in_trash, created
	FROM recipes
	WHERE in_trash = 0
	`

	args := []interface{}{}

	if searchQuery != "" {
		// Parse which fields to search in
		fields := []string{}
		if searchIn == "all" || searchIn == "" {
			fields = []string{"name", "ingredients", "directions", "description", "notes"}
		} else {
			// Split comma-separated field list, excluding categories since they're in a different table
			for _, field := range strings.Split(searchIn, ",") {
				trimmed := strings.TrimSpace(field)
				if trimmed != "categories" {
					fields = append(fields, trimmed)
				}
			}
		}

		// Build OR conditions for each field
		if len(fields) > 0 {
			conditions := []string{}
			for _, field := range fields {
				conditions = append(conditions, fmt.Sprintf("%s LIKE ?", field))
				args = append(args, "%"+searchQuery+"%")
			}

			searchCondition := " AND (" + strings.Join(conditions, " OR ") + ")"
			countQuery += searchCondition
			query += searchCondition
		}
	}

	// Add rating filter if specified
	if minRating > 0 {
		ratingCondition := " AND rating >= ?"
		countQuery += ratingCondition
		query += ratingCondition
		args = append(args, minRating)
	}

	// Get total count
	var totalCount int
	err := r.db.QueryRow(countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get recipe count: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY name"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query recipes: %w", err)
	}
	defer rows.Close()

	var recipes []*paprika.Recipe
	recipeUIDs := []string{}

	for rows.Next() {
		var recipe paprika.Recipe
		var created interface{} // Can be either time.Time or float64

		err := rows.Scan(
			&recipe.UID,
			&recipe.Name,
			&recipe.Ingredients,
			&recipe.Directions,
			&recipe.Description,
			&recipe.Notes,
			&recipe.Servings,
			&recipe.PrepTime,
			&recipe.CookTime,
			&recipe.Difficulty,
			&recipe.Rating,
			&recipe.InTrash,
			&created,
		)
		if err != nil {
			r.logger.Error("failed to scan recipe row", "error", err)
			continue
		}

		// Handle created field - can be time.Time or Julian date
		switch v := created.(type) {
		case time.Time:
			recipe.Created = v.Format(time.RFC3339)
		case float64:
			if v > 0 {
				julianEpoch := time.Date(-4713, 11, 24, 12, 0, 0, 0, time.UTC)
				createdTime := julianEpoch.Add(time.Duration(v * 24 * float64(time.Hour)))
				recipe.Created = createdTime.Format(time.RFC3339)
			}
		case string:
			// If it's already a string, try to parse it
			if t, err := time.Parse("2006-01-02 15:04:05 -0700 MST", v); err == nil {
				recipe.Created = t.Format(time.RFC3339)
			} else if t, err := time.Parse(time.RFC3339, v); err == nil {
				recipe.Created = t.Format(time.RFC3339)
			} else {
				recipe.Created = v
			}
		}

		recipeUIDs = append(recipeUIDs, recipe.UID)
		recipes = append(recipes, &recipe)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating recipes: %w", err)
	}

	// Get categories for all recipes
	if err := r.loadRecipeCategories(recipes); err != nil {
		r.logger.Error("failed to load categories", "error", err)
		// Continue without categories rather than failing
	}

	return recipes, totalCount, nil
}

func (r *RecipeDB) GetRecipeCount() (int, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM recipes WHERE in_trash = 0").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get recipe count: %w", err)
	}
	return count, nil
}

func (r *RecipeDB) GetCategoryNames(uids []string) (map[string]string, error) {
	if len(uids) == 0 {
		return map[string]string{}, nil
	}

	// Build query with placeholders for IN clause using Paprika's recipe_categories table
	placeholders := strings.Repeat("?,", len(uids))
	placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma

	query := fmt.Sprintf("SELECT uid, name FROM recipe_categories WHERE uid IN (%s)", placeholders)

	// Convert []string to []interface{} for query args
	args := make([]interface{}, len(uids))
	for i, uid := range uids {
		args[i] = uid
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query categories: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var uid, name string
		if err := rows.Scan(&uid, &name); err != nil {
			r.logger.Error("failed to scan category row", "error", err)
			continue
		}
		result[uid] = name
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating categories: %w", err)
	}

	return result, nil
}

func (r *RecipeDB) loadRecipeCategories(recipes []*paprika.Recipe) error {
	if len(recipes) == 0 {
		return nil
	}

	// Build list of recipe UIDs
	recipeUIDs := make([]string, len(recipes))
	recipeMap := make(map[string]*paprika.Recipe)
	for i, recipe := range recipes {
		recipeUIDs[i] = recipe.UID
		recipeMap[recipe.UID] = recipe
		recipe.Categories = []string{} // Initialize empty
	}

	// Query junction table for category relationships
	placeholders := strings.Repeat("?,", len(recipeUIDs))
	placeholders = placeholders[:len(placeholders)-1]

	query := fmt.Sprintf(`
		SELECT rtc.recipe_uid, rc.name
		FROM recipes_to_categories rtc
		JOIN recipe_categories rc ON rtc.category_uid = rc.uid
		WHERE rtc.recipe_uid IN (%s)
		ORDER BY rtc.recipe_uid, rc.order_flag
	`, placeholders)

	args := make([]interface{}, len(recipeUIDs))
	for i, uid := range recipeUIDs {
		args[i] = uid
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return fmt.Errorf("failed to query recipe categories: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var recipeUID, categoryName string
		if err := rows.Scan(&recipeUID, &categoryName); err != nil {
			r.logger.Error("failed to scan category row", "error", err)
			continue
		}

		if recipe, ok := recipeMap[recipeUID]; ok {
			recipe.Categories = append(recipe.Categories, categoryName)
		}
	}

	return rows.Err()
}

func (r *RecipeDB) GetMenuItems(startDate, endDate string) ([]map[string]interface{}, error) {
	// Use Paprika's meals table
	query := `
	SELECT uid, recipe_uid, name, date, type_uid, order_flag
	FROM meals
	WHERE date >= ? AND date <= ?
	ORDER BY date, order_flag
	`

	rows, err := r.db.Query(query, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query meals: %w", err)
	}
	defer rows.Close()

	var items []map[string]interface{}
	for rows.Next() {
		var uid, recipeUID, name, typeUID string
		var dateFloat float64
		var orderFlag int

		if err := rows.Scan(&uid, &recipeUID, &name, &dateFloat, &typeUID, &orderFlag); err != nil {
			r.logger.Error("failed to scan meal row", "error", err)
			continue
		}

		// Convert Julian date to string
		var dateStr string
		if dateFloat > 0 {
			julianEpoch := time.Date(-4713, 11, 24, 12, 0, 0, 0, time.UTC)
			date := julianEpoch.Add(time.Duration(dateFloat * 24 * float64(time.Hour)))
			dateStr = date.Format("2006-01-02")
		}

		items = append(items, map[string]interface{}{
			"uid":        uid,
			"recipe_uid": recipeUID,
			"name":       name,
			"date":       dateStr,
			"type_uid":   typeUID,
			"order_flag": orderFlag,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating meals: %w", err)
	}

	return items, nil
}

func (r *RecipeDB) GetGroceryItems(purchasedOnly bool) ([]map[string]interface{}, error) {
	// Use Paprika's actual grocery_items schema
	query := `
	SELECT uid, name, order_flag, purchased, aisle_name, aisle_uid,
		   ingredient, quantity, recipe_name, instruction, list_uid
	FROM grocery_items
	`

	if purchasedOnly {
		query += " WHERE purchased = 0"
	}

	query += " ORDER BY purchased, aisle_name, order_flag, name"

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query grocery items: %w", err)
	}
	defer rows.Close()

	var items []map[string]interface{}
	for rows.Next() {
		var uid, name, aisleName, aisleUID, ingredient, quantity, recipeName, instruction, listUID string
		var orderFlag int
		var purchased bool

		if err := rows.Scan(&uid, &name, &orderFlag, &purchased, &aisleName, &aisleUID,
			&ingredient, &quantity, &recipeName, &instruction, &listUID); err != nil {
			r.logger.Error("failed to scan grocery item row", "error", err)
			continue
		}

		items = append(items, map[string]interface{}{
			"uid":         uid,
			"name":        name,
			"order_flag":  orderFlag,
			"purchased":   purchased,
			"aisle":       aisleName,
			"aisle_uid":   aisleUID,
			"ingredient":  ingredient,
			"quantity":    quantity,
			"recipe":      recipeName,
			"instruction": instruction,
			"list_uid":    listUID,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating grocery items: %w", err)
	}

	return items, nil
}

func (r *RecipeDB) Close() error {
	return r.db.Close()
}
