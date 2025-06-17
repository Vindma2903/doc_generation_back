package auth

import (
	"database/sql"
	"time"
)

var db *sql.DB // локальная переменная пакета auth

func InitAuth(database *sql.DB) {
	db = database
}

type User struct {
	ID           int
	Email        string
	PasswordHash string
	FirstName    string
	LastName     string
}

func getUserByEmail(email string) (*User, error) {
	row := db.QueryRow(`
		SELECT id, email, password_hash, first_name, last_name 
		FROM users WHERE email = $1
	`, email)

	var user User
	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.FirstName, &user.LastName); err != nil {
		return nil, err
	}

	return &user, nil
}

func createUser(firstName, lastName, email, passwordHash string) error {
	_, err := db.Exec(`
		INSERT INTO users (first_name, last_name, email, password_hash)
		VALUES ($1, $2, $3, $4)
	`, firstName, lastName, email, passwordHash)
	return err
}

func createSession(userID int, token string, expiresAt time.Time) error {
	_, err := db.Exec(`
        INSERT INTO sessions (user_id, jwt_token, expires_at)
        VALUES ($1, $2, $3)
    `, userID, token, expiresAt)
	return err
}

func getUserByID(id int) (*User, error) {
	row := db.QueryRow(`
		SELECT id, email, password_hash, first_name, last_name 
		FROM users WHERE id = $1
	`, id)

	var user User
	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.FirstName, &user.LastName); err != nil {
		return nil, err
	}

	return &user, nil
}
