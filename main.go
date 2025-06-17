package main

import (
	"log"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"doc-generation/auth"
	"doc-generation/document" // 👈 добавь этот импорт
	"doc-generation/templates"
)

func main() {
	InitDB()
	defer DB.Close()

	// Инициализация логики (передаём подключение к БД)
	auth.InitAuth(DB)
	templates.InitTemplates(DB)
	document.InitDocumentRepo(DB) // 👈 инициализируем модуль document

	r := gin.Default()

	// Разрешаем CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Регистрируем маршруты
	auth.RegisterRoutes(r)
	templates.RegisterTemplateRoutes(r)
	document.RegisterDocumentRoutes(r) // 👈 подключаем document маршруты

	log.Println("Сервер запущен на :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
