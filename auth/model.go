package auth

import (
	"database/sql"
	"log"
	"time"
)

var db *sql.DB // локальная переменная пакета auth

func InitAuth(database *sql.DB) {
	db = database
}

type User struct {
	ID                       int
	Email                    string
	PasswordHash             string
	FirstName                string
	LastName                 string
	VerificationTokenExpires sql.NullTime
	OrganizationID           int
	IsOwner                  bool
	EmailVerified            bool
	Role                     string
}

func getUserByEmail(email string) (*User, error) {
	log.Printf("%s 🔍 Поиск пользователя по email: %s\n", time.Now().Format("2006/01/02 15:04:05"), email)

	row := db.QueryRow(`
		SELECT id, email, password_hash, first_name, last_name, 
		       verification_token_expires, organization_id, is_owner, email_verified
		FROM users 
		WHERE email = $1
	`, email)

	var user User
	if err := row.Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.FirstName,
		&user.LastName,
		&user.VerificationTokenExpires,
		&user.OrganizationID,
		&user.IsOwner,
		&user.EmailVerified,
	); err != nil {
		if err == sql.ErrNoRows {
			log.Printf("%s ⚠️ Пользователь с email %s не найден\n", time.Now().Format("2006/01/02 15:04:05"), email)
		} else {
			log.Printf("%s ❌ Ошибка при поиске пользователя с email %s: %v\n", time.Now().Format("2006/01/02 15:04:05"), email, err)
		}
		return nil, err
	}

	log.Printf("%s ✅ Найден пользователь: ID=%d, Email=%s\n", time.Now().Format("2006/01/02 15:04:05"), user.ID, user.Email)
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
		SELECT id, email, password_hash, first_name, last_name,
		       verification_token_expires, organization_id, is_owner, email_verified
		FROM users WHERE id = $1
	`, id)

	var user User
	if err := row.Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.FirstName,
		&user.LastName,
		&user.VerificationTokenExpires,
		&user.OrganizationID,
		&user.IsOwner,
		&user.EmailVerified,
	); err != nil {
		return nil, err
	}

	return &user, nil
}

func createUserWithVerification(
	firstName, lastName, email, passwordHash, token string,
	expires time.Time, orgID int, isOwner bool,
) error {
	_, err := db.Exec(`
		INSERT INTO users (
			first_name, last_name, email, password_hash,
			verification_token, verification_token_expires,
			email_verified, organization_id, is_owner
		)
		VALUES ($1, $2, $3, $4, $5, $6, false, $7, $8)
	`, firstName, lastName, email, passwordHash, token, expires, orgID, isOwner)
	return err
}

func getUsersByOrganizationID(orgID int) ([]User, error) {
	rows, err := db.Query(`
		SELECT id, email, first_name, last_name, verification_token_expires,
		       organization_id, is_owner, email_verified, role
		FROM users
		WHERE organization_id = $1
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		err := rows.Scan(
			&u.ID,
			&u.Email,
			&u.FirstName,
			&u.LastName,
			&u.VerificationTokenExpires,
			&u.OrganizationID,
			&u.IsOwner,
			&u.EmailVerified,
			&u.Role, // ← добавлено!
		)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func updateUserRole(userID int, newRole string) error {
	_, err := db.Exec(`
		UPDATE users
		SET role = $1
		WHERE id = $2
	`, newRole, userID)
	return err
}

func getAllUsers() ([]User, error) {
	rows, err := db.Query("SELECT id, first_name, last_name, role FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.FirstName, &u.LastName, &u.Role); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}
