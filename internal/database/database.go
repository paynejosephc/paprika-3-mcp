package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
	// Ensure the directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	rdb := &RecipeDB{
		db:     db,
		logger: logger,
	}

	if err := rdb.createTables(); err != nil {
		db.Close()
		return nil, err
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
	// Build query with search filtering
	countQuery := "SELECT COUNT(*) FROM recipes WHERE in_trash = 0"
	query := `
	SELECT uid, name, ingredients, directions, description, notes,
		   servings, prep_time, cook_time, difficulty, categories,
		   rating, in_trash, created_at
	FROM recipes
	WHERE in_trash = 0
	`

	args := []interface{}{}

	if searchQuery != "" {
		// Parse which fields to search in
		fields := []string{}
		if searchIn == "all" || searchIn == "" {
			fields = []string{"name", "ingredients", "directions", "description", "notes", "categories"}
		} else {
			// Split comma-separated field list
			for _, field := range strings.Split(searchIn, ",") {
				fields = append(fields, strings.TrimSpace(field))
			}
		}

		// Build OR conditions for each field
		conditions := []string{}
		for _, field := range fields {
			conditions = append(conditions, fmt.Sprintf("%s LIKE ?", field))
			args = append(args, "%"+searchQuery+"%")
		}

		searchCondition := " AND (" + strings.Join(conditions, " OR ") + ")"
		countQuery += searchCondition
		query += searchCondition
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
	var allCategoryUIDs []string
	categoryUIDMap := make(map[int][]string) // recipe index -> category UIDs

	for rows.Next() {
		var recipe paprika.Recipe
		var createdAt time.Time
		var categoriesStr string

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
			&categoriesStr,
			&recipe.Rating,
			&recipe.InTrash,
			&createdAt,
		)
		if err != nil {
			r.logger.Error("failed to scan recipe row", "error", err)
			continue
		}

		recipe.Created = createdAt.Format(time.RFC3339)

		// Store category UIDs for later lookup
		if categoriesStr != "" {
			uids := strings.Split(categoriesStr, ",")
			categoryUIDMap[len(recipes)] = uids
			allCategoryUIDs = append(allCategoryUIDs, uids...)
		}

		recipes = append(recipes, &recipe)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating recipes: %w", err)
	}

	// Batch lookup category names
	categoryNames, err := r.GetCategoryNames(allCategoryUIDs)
	if err != nil {
		r.logger.Error("failed to get category names", "error", err)
		// Continue without category names rather than failing
	}

	// Map category names back to recipes
	for i, recipe := range recipes {
		if uids, ok := categoryUIDMap[i]; ok {
			names := make([]string, 0, len(uids))
			for _, uid := range uids {
				if name, found := categoryNames[uid]; found {
					names = append(names, name)
				} else {
					// If name not found, use UID as fallback
					names = append(names, uid)
				}
			}
			recipe.Categories = names
		} else {
			recipe.Categories = []string{}
		}
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

	// Build query with placeholders for IN clause
	placeholders := strings.Repeat("?,", len(uids))
	placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma

	query := fmt.Sprintf("SELECT uid, name FROM categories WHERE uid IN (%s)", placeholders)

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
