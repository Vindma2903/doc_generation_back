package templates

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"
)

var db *sql.DB

func InitTemplates(database *sql.DB) {
	db = database
}

type Template struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// Create
func CreateTemplate(userID int, name, content string) (int, error) {
	var newID int
	err := db.QueryRow(`
		INSERT INTO templates (user_id, name, content)
		VALUES ($1, $2, $3)
		RETURNING id
	`, userID, name, content).Scan(&newID)

	if err != nil {
		return 0, err
	}
	return newID, nil
}

// Get by ID
func GetTemplateByID(id int) (*Template, error) {
	row := db.QueryRow(`SELECT id, user_id, name, content, created_at FROM templates WHERE id = $1`, id)

	var t Template
	err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.Content, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Update
func UpdateTemplate(id int, name, content string) error {
	_, err := db.Exec(`UPDATE templates SET name = $1, content = $2 WHERE id = $3`, name, content, id)
	return err
}

// Delete
func DeleteTemplate(id int) error {
	_, err := db.Exec(`DELETE FROM templates WHERE id = $1`, id)
	return err
}

func GetAllTemplates() ([]TemplateWithCreator, error) {
	rows, err := db.Query(`
		SELECT t.id, t.name, t.created_at, u.first_name, u.last_name
		FROM templates t
		JOIN users u ON t.user_id = u.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []TemplateWithCreator
	for rows.Next() {
		var tmpl TemplateWithCreator
		if err := rows.Scan(&tmpl.ID, &tmpl.Name, &tmpl.CreatedAt, &tmpl.Creator.FirstName, &tmpl.Creator.LastName); err != nil {
			return nil, err
		}
		templates = append(templates, tmpl)
	}

	return templates, nil
}

type TemplateWithCreator struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Creator   struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	} `json:"creator"`
}

func RenameTemplate(id int, name string) error {
	_, err := db.Exec("UPDATE templates SET name = $1 WHERE id = $2", name, id)
	return err
}

func UpdateTemplateContent(id int, content string) error {
	log.Printf("🔁 Обновление контента шаблона ID=%d, длина контента=%d", id, len(content))
	query := `UPDATE templates SET content = $1 WHERE id = $2`
	result, err := db.Exec(query, content, id)
	if err != nil {
		log.Printf("❌ SQL-ошибка при обновлении шаблона ID=%d: %v", id, err)
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("❓ Не удалось получить число затронутых строк: %v", err)
		return err
	}

	if rowsAffected == 0 {
		log.Printf("⚠️ Шаблон с ID=%d не найден, обновление не выполнено", id)
	} else {
		log.Printf("✅ Контент шаблона ID=%d успешно обновлён, строк изменено: %d", id, rowsAffected)
	}

	return nil
}

type Tag struct {
	ID          int       `json:"id" db:"id"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	Name        string    `json:"name" db:"name"`
	Label       string    `json:"label" db:"label"`
	Description string    `json:"description" db:"description"`
	Type        string    `json:"type" db:"type"` // 👈 новое поле
}

// CreateTag создает новый тег в таблице tags
func CreateTag(name, label, description, tagType string) (*Tag, error) {
	var tag Tag
	query := `
		INSERT INTO tags (name, label, description, type, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		RETURNING id, name, label, description, type, created_at
	`
	err := db.QueryRow(query, name, label, description, tagType).Scan(
		&tag.ID, &tag.Name, &tag.Label, &tag.Description, &tag.Type, &tag.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// GetAllTags возвращает все теги из таблицы tags
func GetAllTags() ([]Tag, error) {
	rows, err := db.Query(`SELECT id, name, label, description, type, created_at FROM tags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var t Tag
		err := rows.Scan(
			&t.ID,
			&t.Name,
			&t.Label,
			&t.Description,
			&t.Type, // 👈 добавлено считывание типа
			&t.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, nil
}

type UpdateTagRequest struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Type        string `json:"type"` // 👈 новое поле
}

func UpdateTag(id string, name, label, description, tagType string) (*Tag, error) {
	var tag Tag
	query := `
		UPDATE tags
		SET name = $1, label = $2, description = $3, type = $4
		WHERE id = $5
		RETURNING id, name, label, description, type, created_at
	`
	err := db.QueryRow(query, name, label, description, tagType, id).Scan(
		&tag.ID, &tag.Name, &tag.Label, &tag.Description, &tag.Type, &tag.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

type TemplateStyle struct {
	ID         int                    `json:"id"`
	TemplateID int                    `json:"template_id"`
	Selector   string                 `json:"selector"`
	Styles     map[string]interface{} `json:"styles"`
	Scope      string                 `json:"scope"` // 👈 добавлено
	CreatedAt  time.Time              `json:"created_at"`
}

// CreateTemplateStyle добавляет стиль к шаблону
func CreateTemplateStyle(templateID int, selector string, styles map[string]interface{}) error {
	stylesJSON, err := json.Marshal(styles)
	if err != nil {
		log.Println("❌ Marshal error:", err)
		return err
	}

	_, err = db.Exec(`
		INSERT INTO template_styles (template_id, selector, styles)
		VALUES ($1, $2, $3)
		ON CONFLICT (template_id, selector) DO UPDATE
		SET styles = EXCLUDED.styles
	`, templateID, selector, stylesJSON)

	if err != nil {
		log.Println("❌ SQL Exec error:", err)
	}

	return err
}

// GetStylesByTemplateID возвращает все стили по шаблону
func GetStylesByTemplateID(templateID int) ([]TemplateStyle, error) {
	rows, err := db.Query(`
		SELECT id, template_id, selector, styles, scope, created_at
		FROM template_styles
		WHERE template_id = $1
	`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TemplateStyle
	for rows.Next() {
		var s TemplateStyle
		var stylesData []byte
		err := rows.Scan(&s.ID, &s.TemplateID, &s.Selector, &stylesData, &s.Scope, &s.CreatedAt)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(stylesData, &s.Styles)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}
