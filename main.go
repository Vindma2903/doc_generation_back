package main

import (
	"log"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"doc-generation/auth"
	"doc-generation/config"
	"doc-generation/document"
	"doc-generation/templates"
)

func main() {
	// Загружаем переменные из .env
	config.LoadEnv()

	// Инициализация базы данных
	InitDB()
	defer DB.Close()

	// Инициализация бизнес-логики с подключением к БД
	auth.InitAuth(DB)
	templates.InitTemplates(DB)
	document.InitDocumentRepo(DB)

	// Создаём маршрутизатор Gin
	r := gin.Default()

	// CORS-настройки (читаем из .env)
	frontendURL := config.GetEnv("FRONTEND_URL", "http://localhost:3000")
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{frontendURL},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Регистрируем маршруты каждого модуля
	auth.RegisterRoutes(r)
	templates.RegisterTemplateRoutes(r)
	document.RegisterDocumentRoutes(r)

	// Стартуем сервер
	port := config.GetEnv("PORT", "8080")
	log.Println("Сервер запущен на порту :" + port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
