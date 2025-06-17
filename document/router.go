package document

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	// Импорт модели из текущего пакета, так как файл model.go тоже в пакете `document`
	// НЕ нужно использовать alias вроде `model`, можно вызывать напрямую
)

// RegisterDocumentRoutes подключает все маршруты, связанные с документами
func RegisterDocumentRoutes(r *gin.Engine) {
	r.POST("/documents/update-content", UpdateDocumentContentHandler)
	r.POST("/documents/:id/revision", SaveDocumentRevisionHandler)
	r.POST("/documents/create", CreateDocumentHandler)
	r.GET("/documents/:id", GetDocumentByIDHandler)
	r.GET("/documents/:id/data", GetDocumentDataHandler)
	r.POST("/documents/:id/data", SaveDocumentFieldHandler)
	r.GET("/documents/user/:id", GetDocumentsByUserHandler)

}

// --- Запрос для обновления контента ---
type UpdateDocumentContentRequest struct {
	ID      int    `json:"id"`
	Content string `json:"content"`
}

// Обработчик обновления контента документа
// Обновление финального render-контента (только rendered_content)
func UpdateDocumentContentHandler(c *gin.Context) {
	var req UpdateDocumentContentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный JSON"})
		return
	}

	err := UpdateRenderedContent(req.ID, req.Content)
	if err != nil {
		log.Printf("❌ Ошибка при сохранении rendered_content: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при сохранении контента"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "rendered content updated"})
}

// --- Запрос для сохранения ревизии ---
type SaveRevisionRequest struct {
	Content string `json:"content"`
}

// Обработчик сохранения новой ревизии
func SaveDocumentRevisionHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный ID документа"})
		return
	}

	var req SaveRevisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	err = SaveDocumentRevision(id, req.Content)
	if err != nil {
		log.Printf("❌ Ошибка сохранения ревизии: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при сохранении версии"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "revision saved"})
}

// POST /documents/create
type CreateDocumentRequest struct {
	UserID     int `json:"user_id"`
	TemplateID int `json:"template_id"`
}

type CreateDocumentResponse struct {
	DocumentID int `json:"document_id"`
}

func CreateDocumentHandler(c *gin.Context) {
	var req CreateDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	var newID int
	err := db.QueryRow(`
		INSERT INTO documents (user_id, template_id, name, content)
		SELECT $1, $2, t.name, t.content FROM templates t WHERE t.id = $2
		RETURNING id
	`, req.UserID, req.TemplateID).Scan(&newID)

	if err != nil {
		log.Printf("❌ Ошибка создания документа: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать документ"})
		return
	}

	c.JSON(http.StatusOK, CreateDocumentResponse{DocumentID: newID})
}

// GetDocumentByIDHandler возвращает документ по его ID
func GetDocumentByIDHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный ID"})
		return
	}

	row := db.QueryRow(`
		SELECT id, user_id, template_id, name, content, rendered_content, created_at
		FROM documents
		WHERE id = $1
	`, id)

	var rendered sql.NullString
	var doc struct {
		ID              int       `json:"id"`
		UserID          int       `json:"user_id"`
		TemplateID      int       `json:"template_id"`
		Name            string    `json:"name"`
		Content         string    `json:"content"`
		RenderedContent string    `json:"rendered_content"`
		CreatedAt       time.Time `json:"created_at"`
	}

	err = row.Scan(&doc.ID, &doc.UserID, &doc.TemplateID, &doc.Name, &doc.Content, &rendered, &doc.CreatedAt)
	if err != nil {
		log.Printf("❌ Документ не найден: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Документ не найден"})
		return
	}

	// если rendered_content = NULL, заполним пустой строкой
	if rendered.Valid {
		doc.RenderedContent = rendered.String
	} else {
		doc.RenderedContent = ""
	}

	c.JSON(http.StatusOK, doc)
}

// GetDocumentDataHandler возвращает все заполненные поля по document_id
func GetDocumentDataHandler(c *gin.Context) {
	idStr := c.Param("id")
	documentID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный ID"})
		return
	}

	data, err := GetDocumentData(documentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось получить данные"})
		return
	}

	c.JSON(http.StatusOK, data)
}

// SaveDocumentFieldHandler сохраняет или обновляет значение конкретного поля
func SaveDocumentFieldHandler(c *gin.Context) {
	idStr := c.Param("id")
	documentID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный ID"})
		return
	}

	var req struct {
		FieldName  string `json:"field_name"`
		FieldValue string `json:"field_value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.FieldName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный запрос"})
		return
	}

	err = SaveOrUpdateDocumentField(documentID, req.FieldName, req.FieldValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось сохранить поле"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "saved"})
}

// GetDocumentsByUserHandler возвращает все документы пользователя
func GetDocumentsByUserHandler(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный ID пользователя"})
		return
	}

	rows, err := db.Query(`
		SELECT id, user_id, template_id, name, content, rendered_content, created_at
		FROM documents
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		log.Printf("❌ Ошибка при получении документов: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось получить документы"})
		return
	}
	defer rows.Close()

	var documents []map[string]interface{}
	for rows.Next() {
		var rendered sql.NullString
		var doc struct {
			ID         int
			UserID     int
			TemplateID int
			Name       string
			Content    string
			CreatedAt  time.Time
		}

		err := rows.Scan(&doc.ID, &doc.UserID, &doc.TemplateID, &doc.Name, &doc.Content, &rendered, &doc.CreatedAt)
		if err != nil {
			log.Printf("❌ Ошибка при чтении строки: %v", err)
			continue
		}

		documents = append(documents, gin.H{
			"id":               doc.ID,
			"user_id":          doc.UserID,
			"template_id":      doc.TemplateID,
			"name":             doc.Name,
			"content":          doc.Content,
			"rendered_content": rendered.String,
			"created_at":       doc.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, documents)
}
