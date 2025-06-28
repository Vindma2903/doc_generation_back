package document

import (
	"baliance.com/gooxml/document"

	"database/sql"
	"fmt"
	"golang.org/x/net/html"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"baliance.com/gooxml/measurement"
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
	r.POST("/documents/:id/export-word", ExportDocumentToWordHandler)
	r.GET("/documents/:id/export", ExportDocxHandler)
	r.GET("/documents/:id/export-docx", ExportDocxHandler)

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

func ExportDocumentToWordHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный ID"})
		return
	}

	// Получаем rendered_content из БД
	row := db.QueryRow(`SELECT rendered_content FROM documents WHERE id = $1`, id)
	var rendered sql.NullString
	if err := row.Scan(&rendered); err != nil || !rendered.Valid {
		c.JSON(http.StatusNotFound, gin.H{"error": "Документ не найден или пуст"})
		return
	}

	// Создаём временный HTML-файл
	tmpDir := os.TempDir()
	htmlPath := filepath.Join(tmpDir, fmt.Sprintf("document_%d.html", id))
	docxPath := filepath.Join(tmpDir, fmt.Sprintf("document_%d.docx", id))

	if err := os.WriteFile(htmlPath, []byte(rendered.String), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка записи HTML"})
		return
	}

	// Вызываем Pandoc
	cmd := exec.Command("pandoc", htmlPath, "-o", docxPath)
	if err := cmd.Run(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка вызова pandoc"})
		return
	}

	// Отправляем файл пользователю
	c.FileAttachment(docxPath, fmt.Sprintf("document_%d.docx", id))

	// (необязательно) удаляем временные файлы — если хочешь подчистить
	// defer os.Remove(htmlPath)
	// defer os.Remove(docxPath)
}

func ConvertHTMLToWord(doc *document.Document, htmlStr string, styleMap map[string]int) error {
	root, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return err
	}

	var walk func(n *html.Node, p document.Paragraph)

	walk = func(n *html.Node, p document.Paragraph) {
		switch {
		case n.Type == html.ElementNode && n.Data == "p":
			p = doc.AddParagraph()

			for _, attr := range n.Attr {
				if attr.Key == "style" {
					if indent := parseIndent(attr.Val); indent > 0 {
						p.Properties().SetFirstLineIndent(measurement.Distance(indent))
					}
				}
			}

			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, p)
			}

		case n.Type == html.ElementNode && n.Data == "span":
			run := p.AddRun()
			var styleID string
			for _, attr := range n.Attr {
				if attr.Key == "data-style-id" {
					styleID = attr.Val
					break
				}
			}
			if pt, ok := styleMap[styleID]; ok && pt > 0 {
				run.Properties().SetSize(measurement.Distance(pt * 2))
			}

			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					run.AddText(c.Data)
				} else {
					walk(c, p)
				}
			}

		case n.Type == html.TextNode:
			p.AddRun().AddText(n.Data)

		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, p)
			}
		}
	}

	walk(root, document.Paragraph{}) // пустой старт
	return nil
}

func GenerateDocxFromRendered(documentID int, renderedHTML string) (string, error) {
	doc := document.New()

	// Получаем карту styleID → font_size_pt
	styleMap, err := GetFontSizesByDocumentID(documentID)
	if err != nil {
		return "", fmt.Errorf("ошибка получения размеров шрифта: %w", err)
	}

	if err := ConvertHTMLToWord(doc, renderedHTML, styleMap); err != nil {
		return "", fmt.Errorf("ошибка генерации Word-документа: %w", err)
	}

	outputDir := "exports"
	if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
		return "", fmt.Errorf("ошибка создания директории: %w", err)
	}

	filename := fmt.Sprintf("document_%d.docx", documentID)
	path := filepath.Join(outputDir, filename)

	if err := doc.SaveToFile(path); err != nil {
		return "", fmt.Errorf("ошибка сохранения docx: %w", err)
	}

	return path, nil
}

func ExportDocxHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный ID"})
		return
	}

	row := db.QueryRow(`SELECT rendered_content FROM documents WHERE id = $1`, id)
	var rendered sql.NullString
	if err := row.Scan(&rendered); err != nil || !rendered.Valid {
		c.JSON(http.StatusNotFound, gin.H{"error": "Документ не найден или пуст"})
		return
	}

	path, err := GenerateDocxFromRendered(id, rendered.String)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка создания docx"})
		return
	}

	c.FileAttachment(path, fmt.Sprintf("document_%d.docx", id))
}

// Парсит text-indent: 5.25em и возвращает значение в twips (1/20 pt)
func parseIndent(style string) int {
	re := regexp.MustCompile(`text-indent:\s*([\d.]+)em`)
	matches := re.FindStringSubmatch(style)
	if len(matches) >= 2 {
		em, _ := strconv.ParseFloat(matches[1], 64)
		return int(em * 20 * 12) // эм × 12pt × 20 (в twips)
	}
	return 0
}

func GetFontSizesByDocumentID(docID int) (map[string]int, error) {
	rows, err := db.Query(`
		SELECT ts.selector, ts.font_size_pt
		FROM template_styles ts
		JOIN documents d ON ts.template_id = d.template_id
		WHERE d.id = $1 AND ts.scope = 'inline'
	`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	styleMap := make(map[string]int)
	re := regexp.MustCompile(`data-style-id="([^"]+)"`)

	for rows.Next() {
		var selector string
		var size int
		if err := rows.Scan(&selector, &size); err == nil {
			if matches := re.FindStringSubmatch(selector); len(matches) == 2 {
				styleID := matches[1]
				styleMap[styleID] = size
			}
		}
	}
	return styleMap, nil
}
