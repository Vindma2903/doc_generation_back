package templates

import (
	"database/sql"
	"github.com/goccy/go-json"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func RegisterTemplateRoutes(r *gin.Engine) {
	r.POST("/templates/create", createTemplateHandler)
	r.GET("/templates/get", getTemplateHandler)
	r.PUT("/templates/update", updateTemplateHandler)
	r.DELETE("/templates/delete", deleteTemplateHandler)
	r.GET("/templates/all", getAllTemplatesHandler)
	r.GET("/templates/:id", getTemplateByIDHandler)
	r.POST("/templates/rename", renameTemplateHandler)
	r.PUT("/templates/update-content", updateTemplateContentHandler)
	r.POST("/tags/create", createTagHandler)
	r.GET("/tags/all", getAllTagsHandler)
	r.PUT("/tags/:id", updateTagHandler)
	r.POST("/templates/styles", createTemplateStyleHandler)
	r.GET("/templates/:id/styles", getTemplateStylesHandler)

}

// ----------------- Create -----------------

type CreateRequest struct {
	UserID  int    `json:"user_id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func createTemplateHandler(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Println("Ошибка биндинга JSON:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	log.Printf("Получен шаблон: %+v\n", req)

	newID, err := CreateTemplate(req.UserID, req.Name, req.Content)
	if err != nil {
		log.Println("Ошибка при создании шаблона:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при создании шаблона"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": newID})
}

// ----------------- Get -----------------

func getTemplateHandler(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный ID"})
		return
	}

	t, err := GetTemplateByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Шаблон не найден"})
		return
	}

	c.JSON(http.StatusOK, t)
}

// ----------------- Update -----------------

type UpdateRequest struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func updateTemplateHandler(c *gin.Context) {
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	if err := UpdateTemplate(req.ID, req.Name, req.Content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при обновлении шаблона"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Шаблон обновлён"})
}

// ----------------- Delete -----------------

func deleteTemplateHandler(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный ID"})
		return
	}

	if err := DeleteTemplate(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при удалении шаблона"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Шаблон удалён"})
}

func getAllTemplatesHandler(c *gin.Context) {
	templates, err := GetAllTemplates() // Эта функция должна получать все шаблоны из БД
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при получении шаблонов"})
		return
	}

	c.JSON(http.StatusOK, templates)
}

func getTemplateByIDHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный ID"})
		return
	}

	t, err := GetTemplateByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Шаблон не найден"})
		return
	}

	c.JSON(http.StatusOK, t)
}

type RenameTemplateRequest struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func renameTemplateHandler(c *gin.Context) {
	var req RenameTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Название не может быть пустым"})
		return
	}

	if err := RenameTemplate(req.ID, req.Name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при переименовании шаблона"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Название шаблона обновлено"})
}

type UpdateContentRequest struct {
	ID      int    `json:"id"`
	Content string `json:"content"`
}

func updateTemplateContentHandler(c *gin.Context) {
	var req UpdateContentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Println("❌ Ошибка биндинга JSON в updateTemplateContentHandler:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	log.Printf("🔁 Обновление контента шаблона ID=%d, длина контента=%d\n", req.ID, len(req.Content))

	if err := UpdateTemplateContent(req.ID, req.Content); err != nil {
		log.Printf("❌ Ошибка обновления контента шаблона ID=%d: %v\n", req.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при обновлении контента"})
		return
	}

	log.Printf("✅ Контент шаблона ID=%d успешно обновлён\n", req.ID)
	c.JSON(http.StatusOK, gin.H{"message": "Контент обновлён"})
}

type CreateTagRequest struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Type        string `json:"type"` // 👈 новое поле
}

func createTagHandler(c *gin.Context) {
	var req CreateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	if req.Name == "" || req.Label == "" || req.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Имя, метка и тип обязательны"})
		return
	}

	tag, err := CreateTag(req.Name, req.Label, req.Description, req.Type) // 👈 передаем type
	if err != nil {
		log.Printf("❌ Ошибка при создании тега: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при создании тега"})
		return
	}

	c.JSON(http.StatusCreated, tag)
}

func getAllTagsHandler(c *gin.Context) {
	tags, err := GetAllTags()
	if err != nil {
		log.Printf("❌ Ошибка при получении тегов: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при получении тегов"})
		return
	}
	c.JSON(http.StatusOK, tags)
}

func updateTagHandler(c *gin.Context) {
	tagID := c.Param("id")
	if tagID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID тега обязателен"})
		return
	}

	var req UpdateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	if req.Name == "" || req.Label == "" || req.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Имя, метка и тип обязательны"})
		return
	}

	tag, err := UpdateTag(tagID, req.Name, req.Label, req.Description, req.Type) // 👈 передаем type
	if err != nil {
		log.Printf("❌ Ошибка при обновлении тега: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при обновлении тега"})
		return
	}

	c.JSON(http.StatusOK, tag)
}

type CreateStyleRequest struct {
	TemplateID int                    `json:"template_id"`
	Selector   string                 `json:"selector"`
	Styles     map[string]interface{} `json:"styles"`
	Scope      string                 `json:"scope"` // "global" или "inline"
}

func createTemplateStyleHandler(c *gin.Context) {
	var req CreateStyleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	if req.TemplateID == 0 || len(req.Styles) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Все поля обязательны"})
		return
	}

	if req.Scope == "" {
		req.Scope = "global" // по умолчанию
	}

	// Преобразуем стили в JSON
	sanitizedStyles := toKebabCaseStyle(req.Styles)

	stylesJSON, err := json.Marshal(sanitizedStyles)
	if err != nil {
		log.Println("❌ Marshal error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка кодирования стиля"})
		return
	}

	// Проверяем, существует ли уже такой стиль
	var existingSelector string
	err = db.QueryRow(`
		SELECT selector FROM template_styles
		WHERE template_id = $1 AND scope = $2 AND styles::jsonb = $3::jsonb
		LIMIT 1
	`, req.TemplateID, req.Scope, stylesJSON).Scan(&existingSelector)

	if err == nil {
		// Стиль уже существует — возвращаем его selector
		c.JSON(http.StatusOK, gin.H{
			"message":  "Стиль уже существует",
			"selector": existingSelector,
		})
		return
	} else if err != sql.ErrNoRows {
		log.Println("❌ SQL SELECT error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при проверке стиля"})
		return
	}

	// Стиль не найден — создаём
	err = CreateTemplateStyleWithScope(req.TemplateID, req.Selector, sanitizedStyles, req.Scope)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при сохранении стиля"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "Стиль создан",
		"selector": req.Selector,
	})
}

func getTemplateStylesHandler(c *gin.Context) {
	idStr := c.Param("id")
	templateID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный ID"})
		return
	}

	styles, err := GetStylesByTemplateID(templateID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при получении стилей"})
		return
	}

	c.JSON(http.StatusOK, styles)
}

func CreateTemplateStyleWithScope(templateID int, selector string, styles map[string]interface{}, scope string) error {
	stylesJSON, err := json.Marshal(styles)
	if err != nil {
		log.Println("❌ Marshal error:", err)
		return err
	}

	_, err = db.Exec(`
		INSERT INTO template_styles (template_id, selector, styles, scope)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (template_id, selector, scope) DO UPDATE
		SET styles = EXCLUDED.styles
	`, templateID, selector, stylesJSON, scope)

	if err != nil {
		log.Println("❌ SQL Exec error:", err)
	}

	return err
}

func toKebabCaseStyle(input map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range input {
		// fontSize → font-size
		kebabKey := regexp.MustCompile("([a-z0-9])([A-Z])").ReplaceAllString(k, "${1}-${2}")
		result[strings.ToLower(kebabKey)] = v
	}
	return result
}
