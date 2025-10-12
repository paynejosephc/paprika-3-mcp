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

type RecipeRecord struct {
	UID         string
	Name        string
	Ingredients string
	Directions  string
	Description string
	Notes       string
	Servings    string
	PrepTime    string
	CookTime    string
	Difficulty  string
	Categories  string
	Rating      int
	InTrash     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	SyncedAt    time.Time
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

func (r *RecipeDB) createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS recipes (
		uid TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		ingredients TEXT,
		directions TEXT,
		description TEXT,
		notes TEXT,
		servings TEXT,
		prep_time TEXT,
		cook_time TEXT,
		difficulty TEXT,
		categories TEXT,
		rating INTEGER,
		in_trash BOOLEAN DEFAULT 0,
		created_at TIMESTAMP,
		updated_at TIMESTAMP,
		synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_recipes_name ON recipes(name);
	CREATE INDEX IF NOT EXISTS idx_recipes_in_trash ON recipes(in_trash);
	CREATE INDEX IF NOT EXISTS idx_recipes_rating ON recipes(rating);
	CREATE INDEX IF NOT EXISTS idx_recipes_synced_at ON recipes(synced_at);

	CREATE TABLE IF NOT EXISTS categories (
		uid TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		parent_uid TEXT,
		order_flag INTEGER,
		synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_categories_name ON categories(name);

	CREATE TABLE IF NOT EXISTS menu_items (
		uid TEXT PRIMARY KEY,
		recipe_uid TEXT,
		name TEXT NOT NULL,
		date TEXT NOT NULL,
		type INTEGER,
		order_flag INTEGER,
		synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_menu_items_date ON menu_items(date);
	CREATE INDEX IF NOT EXISTS idx_menu_items_recipe_uid ON menu_items(recipe_uid);

	CREATE TABLE IF NOT EXISTS grocery_items (
		uid TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		order_flag INTEGER,
		purchased BOOLEAN DEFAULT 0,
		aisle TEXT,
		aisle_uid TEXT,
		ingredient TEXT,
		quantity TEXT,
		recipe TEXT,
		recipe_uid TEXT,
		instruction TEXT,
		list_uid TEXT,
		synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_grocery_items_purchased ON grocery_items(purchased);
	CREATE INDEX IF NOT EXISTS idx_grocery_items_recipe_uid ON grocery_items(recipe_uid);

	CREATE TABLE IF NOT EXISTS sync_metadata (
		key TEXT PRIMARY KEY,
		value TEXT,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err := r.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	return nil
}

func (r *RecipeDB) UpsertRecipe(recipe *paprika.Recipe) error {
	query := `
	INSERT INTO recipes (
		uid, name, ingredients, directions, description, notes,
		servings, prep_time, cook_time, difficulty, categories,
		rating, in_trash, created_at, updated_at, synced_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(uid) DO UPDATE SET
		name = excluded.name,
		ingredients = excluded.ingredients,
		directions = excluded.directions,
		description = excluded.description,
		notes = excluded.notes,
		servings = excluded.servings,
		prep_time = excluded.prep_time,
		cook_time = excluded.cook_time,
		difficulty = excluded.difficulty,
		categories = excluded.categories,
		rating = excluded.rating,
		in_trash = excluded.in_trash,
		updated_at = excluded.updated_at,
		synced_at = CURRENT_TIMESTAMP
	`

	// Convert categories slice to comma-separated string
	categories := strings.Join(recipe.Categories, ",")

	_, err := r.db.Exec(query,
		recipe.UID,
		recipe.Name,
		recipe.Ingredients,
		recipe.Directions,
		recipe.Description,
		recipe.Notes,
		recipe.Servings,
		recipe.PrepTime,
		recipe.CookTime,
		recipe.Difficulty,
		categories,
		recipe.Rating,
		recipe.InTrash,
		recipe.Created,
		recipe.Created, // Using created as both created_at and updated_at
	)

	if err != nil {
		return fmt.Errorf("failed to upsert recipe: %w", err)
	}

	return nil
}

func (r *RecipeDB) GetRecipe(uid string) (*paprika.Recipe, error) {
	query := `
	SELECT uid, name, ingredients, directions, description, notes,
		   servings, prep_time, cook_time, difficulty, categories,
		   rating, in_trash, created_at
	FROM recipes
	WHERE uid = ? AND in_trash = 0
	`

	var recipe paprika.Recipe
	var createdAt time.Time
	var categoriesStr string

	err := r.db.QueryRow(query, uid).Scan(
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
		&categoriesStr,
		&recipe.Rating,
		&recipe.InTrash,
		&createdAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("recipe not found: %s", uid)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get recipe: %w", err)
	}

	recipe.Created = createdAt.Format(time.RFC3339)

	// Convert category UIDs to names
	if categoriesStr != "" {
		uids := strings.Split(categoriesStr, ",")
		categoryNames, err := r.GetCategoryNames(uids)
		if err != nil {
			r.logger.Error("failed to get category names", "error", err)
			// Use UIDs as fallback
			recipe.Categories = uids
		} else {
			names := make([]string, 0, len(uids))
			for _, uid := range uids {
				if name, found := categoryNames[uid]; found {
					names = append(names, name)
				} else {
					names = append(names, uid)
				}
			}
			recipe.Categories = names
		}
	} else {
		recipe.Categories = []string{}
	}

	return &recipe, nil
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

func (r *RecipeDB) UpsertCategory(uid, name, parentUID string, orderFlag int) error {
	query := `
	INSERT INTO categories (uid, name, parent_uid, order_flag, synced_at)
	VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(uid) DO UPDATE SET
		name = excluded.name,
		parent_uid = excluded.parent_uid,
		order_flag = excluded.order_flag,
		synced_at = CURRENT_TIMESTAMP
	`

	_, err := r.db.Exec(query, uid, name, parentUID, orderFlag)
	if err != nil {
		return fmt.Errorf("failed to upsert category: %w", err)
	}

	return nil
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

func (r *RecipeDB) UpsertMenuItem(uid, recipeUID, name, date string, itemType, orderFlag int) error {
	query := `
	INSERT INTO menu_items (uid, recipe_uid, name, date, type, order_flag, synced_at)
	VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(uid) DO UPDATE SET
		recipe_uid = excluded.recipe_uid,
		name = excluded.name,
		date = excluded.date,
		type = excluded.type,
		order_flag = excluded.order_flag,
		synced_at = CURRENT_TIMESTAMP
	`

	_, err := r.db.Exec(query, uid, recipeUID, name, date, itemType, orderFlag)
	if err != nil {
		return fmt.Errorf("failed to upsert menu item: %w", err)
	}

	return nil
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

func (r *RecipeDB) UpsertGroceryItem(item *paprika.GroceryItem) error {
	query := `
	INSERT INTO grocery_items (
		uid, name, order_flag, purchased, aisle, aisle_uid,
		ingredient, quantity, recipe, recipe_uid, instruction, list_uid, synced_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(uid) DO UPDATE SET
		name = excluded.name,
		order_flag = excluded.order_flag,
		purchased = excluded.purchased,
		aisle = excluded.aisle,
		aisle_uid = excluded.aisle_uid,
		ingredient = excluded.ingredient,
		quantity = excluded.quantity,
		recipe = excluded.recipe,
		recipe_uid = excluded.recipe_uid,
		instruction = excluded.instruction,
		list_uid = excluded.list_uid,
		synced_at = CURRENT_TIMESTAMP
	`

	_, err := r.db.Exec(query,
		item.UID, item.Name, item.OrderFlag, item.Purchased, item.Aisle, item.AisleUID,
		item.Ingredient, item.Quantity, item.Recipe, item.RecipeUID, item.Instruction, item.ListUID,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert grocery item: %w", err)
	}

	return nil
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

func (r *RecipeDB) SetSyncMetadata(key, value string) error {
	query := `
	INSERT INTO sync_metadata (key, value, updated_at)
	VALUES (?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(key) DO UPDATE SET
		value = excluded.value,
		updated_at = CURRENT_TIMESTAMP
	`

	_, err := r.db.Exec(query, key, value)
	if err != nil {
		return fmt.Errorf("failed to set sync metadata: %w", err)
	}
	return nil
}

func (r *RecipeDB) GetSyncMetadata(key string) (string, error) {
	var value string
	err := r.db.QueryRow("SELECT value FROM sync_metadata WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get sync metadata: %w", err)
	}
	return value, nil
}

func (r *RecipeDB) Close() error {
	return r.db.Close()
}
