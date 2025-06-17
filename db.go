package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

var DB *sql.DB

func InitDB() {
	connStr := "host=localhost port=5432 user=postgres password=az19821982 dbname=doc_generation sslmode=disable"
	var err error
	DB, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Ошибка при открытии соединения с БД: %v", err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatalf("Не удалось подключиться к БД: %v", err)
	}

	fmt.Println("Успешное подключение к PostgreSQL")
}
