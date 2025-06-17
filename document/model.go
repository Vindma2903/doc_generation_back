package document

import (
	"database/sql"
	"time"
)

// db — подключение к базе (инициализируется через InitDocumentRepo)
var db *sql.DB

// InitDocumentRepo инициализирует подключение к БД
func InitDocumentRepo(database *sql.DB) {
	db = database
}

// UpdateRenderedContent обновляет только rendered_content
func UpdateRenderedContent(id int, rendered string) error {
	_, err := db.Exec(`UPDATE documents SET rendered_content = $1 WHERE id = $2`, rendered, id)
	return err
}

// DocumentRevision представляет одну сохранённую версию документа
type DocumentRevision struct {
	ID         int       `json:"id"`
	DocumentID int       `json:"document_id"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

// SaveDocumentRevision сохраняет текущую версию документа в историю
func SaveDocumentRevision(documentID int, content string) error {
	_, err := db.Exec(
		`INSERT INTO document_revisions (document_id, content) VALUES ($1, $2)`,
		documentID, content,
	)
	return err
}

// GetDocumentRevisions возвращает список всех версий для документа
func GetDocumentRevisions(documentID int) ([]DocumentRevision, error) {
	rows, err := db.Query(`
		SELECT id, document_id, content, created_at
		FROM document_revisions
		WHERE document_id = $1
		ORDER BY created_at DESC
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var revisions []DocumentRevision
	for rows.Next() {
		var rev DocumentRevision
		if err := rows.Scan(&rev.ID, &rev.DocumentID, &rev.Content, &rev.CreatedAt); err != nil {
			return nil, err
		}
		revisions = append(revisions, rev)
	}
	return revisions, nil
}

type DocumentField struct {
	FieldName  string `json:"field_name"`
	FieldValue string `json:"field_value"`
}

// GetDocumentData возвращает все заполненные поля документа
func GetDocumentData(documentID int) (map[string]string, error) {
	rows, err := db.Query(`SELECT field_name, field_value FROM document_data WHERE document_id = $1`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	data := make(map[string]string)
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		data[name] = value
	}
	return data, nil
}

// SaveOrUpdateDocumentField сохраняет или обновляет значение поля
func SaveOrUpdateDocumentField(documentID int, fieldName, fieldValue string) error {
	_, err := db.Exec(`
		INSERT INTO document_data (document_id, field_name, field_value)
		VALUES ($1, $2, $3)
		ON CONFLICT (document_id, field_name)
		DO UPDATE SET field_value = EXCLUDED.field_value
	`, documentID, fieldName, fieldValue)
	return err
}
