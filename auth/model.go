package auth

import (
	"database/sql"
	"fmt"
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
	RoleID                   int
	RoleName                 string
	IsBlocked                bool `json:"isBlocked"`
}

func getUserByEmail(email string) (*User, error) {
	log.Printf("%s 🔍 Поиск пользователя по email: %s\n", time.Now().Format("2006/01/02 15:04:05"), email)

	row := db.QueryRow(`
		SELECT u.id, u.email, u.password_hash, u.first_name, u.last_name, 
		       u.verification_token_expires, u.organization_id, u.is_owner, 
		       u.email_verified, u.role_id, r.name
		FROM users u
		LEFT JOIN roles r ON u.role_id = r.id
		WHERE u.email = $1
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
		&user.RoleID,
		&user.RoleName, // ← теперь заполняется имя роли
	); err != nil {
		if err == sql.ErrNoRows {
			log.Printf("%s ⚠️ Пользователь с email %s не найден\n", time.Now().Format("2006/01/02 15:04:05"), email)
		} else {
			log.Printf("%s ❌ Ошибка при поиске пользователя с email %s: %v\n", time.Now().Format("2006/01/02 15:04:05"), email, err)
		}
		return nil, err
	}

	log.Printf("%s ✅ Найден пользователь: ID=%d, Email=%s, Role=%s\n", time.Now().Format("2006/01/02 15:04:05"), user.ID, user.Email, user.RoleName)
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
		SELECT u.id, u.email, u.password_hash, u.first_name, u.last_name,
		       u.verification_token_expires, u.organization_id, u.is_owner, u.email_verified,
		       u.role_id, r.name, u.is_blocked
		FROM users u
		LEFT JOIN roles r ON u.role_id = r.id
		WHERE u.id = $1
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
		&user.RoleID,
		&user.RoleName,
		&user.IsBlocked, // 👈 добавлено
	); err != nil {
		return nil, err
	}

	return &user, nil
}

func createUserWithVerification(
	firstName, lastName, email, passwordHash, token string,
	expires time.Time, orgID int, isOwner bool, roleID int,
) error {
	_, err := db.Exec(`
		INSERT INTO users (
			first_name, last_name, email, password_hash,
			verification_token, verification_token_expires,
			email_verified, organization_id, is_owner, role_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, false, $7, $8, $9)
	`, firstName, lastName, email, passwordHash, token, expires, orgID, isOwner, roleID)
	return err
}

func getUsersByOrganizationID(orgID int) ([]User, error) {
	log.Printf("📥 Получение пользователей организации ID: %d", orgID)

	rows, err := db.Query(`
		SELECT u.id, u.email, u.first_name, u.last_name, 
		       u.verification_token_expires, u.organization_id, 
		       u.is_owner, u.email_verified, u.is_blocked,
		       u.role_id, r.name
		FROM users u
		LEFT JOIN roles r ON u.role_id = r.id
		WHERE u.organization_id = $1
	`, orgID)
	if err != nil {
		log.Printf("❌ Ошибка запроса пользователей по организации %d: %v", orgID, err)
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
			&u.IsBlocked,
			&u.RoleID,
			&u.RoleName,
		)
		if err != nil {
			log.Printf("❌ Ошибка чтения строки пользователя: %v", err)
			return nil, err
		}

		log.Printf("👤 Пользователь: ID=%d, Email=%s, Заблокирован=%v, Подтверждён=%v, Роль=%s",
			u.ID, u.Email, u.IsBlocked, u.EmailVerified, u.RoleName)

		users = append(users, u)
	}

	log.Printf("✅ Найдено пользователей: %d", len(users))
	return users, nil
}

func updateUserRole(userID int, roleName string) error {
	var roleID int
	err := db.QueryRow("SELECT id FROM roles WHERE name = $1", roleName).Scan(&roleID)
	if err != nil {
		return fmt.Errorf("роль не найдена: %v", err)
	}

	_, err = db.Exec("UPDATE users SET role_id = $1 WHERE id = $2", roleID, userID)
	if err != nil {
		return fmt.Errorf("ошибка при обновлении роли: %v", err)
	}

	return nil
}

func getAllUsers() ([]User, error) {
	rows, err := db.Query(`
		SELECT u.id, u.first_name, u.last_name, u.role_id, r.name
		FROM users u
		LEFT JOIN roles r ON u.role_id = r.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var roleName string
		if err := rows.Scan(&u.ID, &u.FirstName, &u.LastName, &u.RoleID, &roleName); err != nil {
			return nil, err
		}
		u.RoleName = roleName
		users = append(users, u)
	}
	return users, nil
}

type DeleteRoleRequest struct {
	Name string `json:"name"` // имя роли для удаления
}

type RenameRoleRequest struct {
	OldName string `json:"old_name"` // текущее имя роли
	NewName string `json:"new_name"` // новое имя роли
}

func setUserBlockedStatus(userID int, blocked bool) error {
	_, err := db.Exec(`UPDATE users SET is_blocked = $1 WHERE id = $2`, blocked, userID)
	return err
}
