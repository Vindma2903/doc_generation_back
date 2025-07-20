package auth

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"net/http"

	"crypto/rand"
	"encoding/hex"

	"net/smtp"
	"os"
)

var jwtSecret = []byte("super_secret_key") // ❗ желательно хранить в .env

func RegisterRoutes(r *gin.Engine) {
	r.POST("/login", loginHandler)
	r.POST("/register", registerHandler)
	r.POST("/refresh", refreshHandler)
	r.POST("/verify", verifyHandler)
	r.GET("/verify", verifyEmailHandler)
	r.POST("/invite", AuthMiddleware(), inviteHandler)
	r.POST("/set-password", setPasswordHandler)
	r.GET("/api/users", AuthMiddleware(), getAllUsersHandler)
	r.POST("/roles/rename", renameRoleHandler)
	r.POST("/roles/delete", deleteRoleHandler)

	// ✅ защищённые маршруты через группу
	authGroup := r.Group("/")
	authGroup.Use(AuthMiddleware())
	authGroup.GET("/me", MeHandler)
	authGroup.GET("/auth/check", checkAuthHandler)
	authGroup.GET("/users/invited", getInvitedUsersHandler)
	authGroup.POST("/users/assign-role", assignRoleHandler)
	r.GET("/api/roles", AuthMiddleware(), getAllRolesHandler)
	authGroup.POST("/users/:id/block", blockUserHandler)
	authGroup.DELETE("/users/:id", deleteUserHandler)
	authGroup.POST("/users/:id/unblock", unblockUserHandler)

}

// ----------------- LOGIN -----------------

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func loginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Println("❌ Ошибка парсинга JSON при входе:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	log.Printf("➡️ Попытка входа: email=%s", req.Email)

	user, err := getUserByEmail(req.Email)
	if err != nil || user == nil {
		log.Printf("⚠️ Пользователь не найден: %s", req.Email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Пользователь не найден"})
		return
	}

	if !user.EmailVerified {
		log.Printf("⚠️ Email не подтверждён: %s", req.Email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Email не подтверждён. Проверьте почту."})
		return
	}

	if user.PasswordHash == "" {
		log.Printf("⚠️ Пароль не установлен для пользователя: %s (возможно, приглашён и не завершил регистрацию)", req.Email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Пароль не установлен. Установите пароль через письмо-приглашение."})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		log.Printf("❌ Неверный пароль для пользователя: %s", req.Email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный пароль"})
		return
	}

	log.Printf("🔐 Пароль корректный. Генерация токенов для пользователя ID %d", user.ID)

	accessToken, refreshToken, err := generateTokens(user.ID)
	if err != nil {
		log.Println("❌ Ошибка при генерации токенов:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при создании токенов"})
		return
	}

	log.Println("💾 Сохраняем access токен в сессии...")

	if err := createSession(user.ID, accessToken, time.Now().Add(15*time.Minute)); err != nil {
		log.Println("❌ Ошибка при сохранении сессии:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при сохранении сессии"})
		return
	}

	log.Println("🍪 Устанавливаем refresh_token в HttpOnly cookie...")

	c.SetCookie("refresh_token", refreshToken, 7*24*60*60, "/", "localhost", false, true)

	log.Printf("✅ Вход выполнен успешно: user_id=%d", user.ID)

	c.JSON(http.StatusOK, gin.H{
		"token":   accessToken,
		"user_id": user.ID,
	})
}

// ----------------- REGISTER -----------------

type RegisterRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Password  string `json:"password"`
	Role      string `json:"role"`
}

func getOrCreateRoleID(roleName string) (int, error) {
	var id int
	err := db.QueryRow(`SELECT id FROM roles WHERE name = $1`, roleName).Scan(&id)
	if err == sql.ErrNoRows {
		err = db.QueryRow(`INSERT INTO roles (name) VALUES ($1) RETURNING id`, roleName).Scan(&id)
	}
	return id, err
}

func registerHandler(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("%s ➡️ Ошибка парсинга JSON: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	log.Printf("%s ➡️ Попытка регистрации: email=%s\n", time.Now().Format("2006/01/02 15:04:05"), req.Email)

	if existingUser, _ := getUserByEmail(req.Email); existingUser != nil {
		log.Printf("%s ⚠️ Пользователь с email %s уже существует\n", time.Now().Format("2006/01/02 15:04:05"), req.Email)
		c.JSON(http.StatusConflict, gin.H{"error": "Пользователь с таким email уже существует"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("%s ❌ Ошибка при хешировании пароля: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при хешировании пароля"})
		return
	}

	verificationToken := generateToken()
	verificationExpires := time.Now().Add(24 * time.Hour)

	orgName := fmt.Sprintf("Компания %s %s", req.FirstName, req.LastName)
	orgID, err := createOrganization(orgName)
	if err != nil {
		log.Printf("%s ❌ Ошибка при создании организации: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при создании организации"})
		return
	}

	var userCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM users WHERE organization_id = $1`, orgID).Scan(&userCount)
	if err != nil {
		log.Printf("%s ❌ Ошибка при подсчёте пользователей в организации: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при проверке владельца"})
		return
	}
	isOwner := userCount == 0

	// 🧠 Устанавливаем роль по умолчанию
	if req.Role == "" {
		if isOwner {
			req.Role = "Владелец"
		} else {
			req.Role = "Сотрудник"
		}
	}

	// 👉 Получаем или создаём ID роли
	roleID, err := getOrCreateRoleID(req.Role)
	if err != nil {
		log.Printf("%s ❌ Ошибка при получении ID роли %s: %v\n", time.Now().Format("2006/01/02 15:04:05"), req.Role, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при обработке роли"})
		return
	}

	log.Printf("%s ℹ️ Регистрация нового пользователя: %s %s (%s), организация ID: %d, is_owner=%v, role_id=%d\n",
		time.Now().Format("2006/01/02 15:04:05"), req.FirstName, req.LastName, req.Email, orgID, isOwner, roleID)

	err = createUserWithVerification(
		req.FirstName,
		req.LastName,
		req.Email,
		string(hashedPassword),
		verificationToken,
		verificationExpires,
		orgID,
		isOwner,
		roleID, // 👈 теперь передаём числовой ID
	)
	if err != nil {
		log.Printf("%s ❌ Ошибка при создании пользователя: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при создании пользователя"})
		return
	}

	err = sendVerificationEmail(req.Email, verificationToken)
	if err != nil {
		log.Printf("%s ❌ Ошибка при отправке email: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось отправить email"})
		return
	}

	log.Printf("%s ✅ Письмо отправлено на %s\n", time.Now().Format("2006/01/02 15:04:05"), req.Email)
	c.JSON(http.StatusCreated, gin.H{"message": "Письмо с подтверждением отправлено"})
}

func generateToken() string {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		log.Printf("%s ❌ Ошибка при генерации токена: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
		return ""
	}
	token := hex.EncodeToString(b)
	log.Printf("%s 🔑 Сгенерирован новый токен подтверждения: %s\n", time.Now().Format("2006/01/02 15:04:05"), token)
	return token
}

func sendVerificationEmail(toEmail string, token string) error {
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT")
	smtpUser := os.Getenv("SMTP_USER")
	smtpPass := os.Getenv("SMTP_PASS")
	frontendURL := os.Getenv("FRONTEND_URL")

	verifyLink := frontendURL + "/verify?token=" + token

	subject := "Подтверждение регистрации"
	body := "Здравствуйте!\n\nПерейдите по ссылке для подтверждения регистрации:\n" + verifyLink + "\n\nЕсли вы не регистрировались — проигнорируйте это письмо."

	msg := "From: " + smtpUser + "\n" +
		"To: " + toEmail + "\n" +
		"Subject: " + subject + "\n\n" + body

	auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)

	log.Printf("%s ✉️ Отправка письма подтверждения на %s\n", time.Now().Format("2006/01/02 15:04:05"), toEmail)
	err := smtp.SendMail(smtpHost+":"+smtpPort, auth, smtpUser, []string{toEmail}, []byte(msg))
	if err != nil {
		log.Printf("%s ❌ Ошибка при отправке письма на %s: %v\n", time.Now().Format("2006/01/02 15:04:05"), toEmail, err)
		return err
	}

	log.Printf("%s ✅ Письмо подтверждения успешно отправлено на %s\n", time.Now().Format("2006/01/02 15:04:05"), toEmail)
	return nil
}

func verifyHandler(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if err := c.ShouldBindJSON(&req); err != nil || req.Token == "" {
		log.Printf("%s ❌ Ошибка: токен не передан или JSON некорректен\n", time.Now().Format("2006/01/02 15:04:05"))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Токен не передан"})
		return
	}

	log.Printf("%s 🔐 POST /verify - получен токен: %s\n", time.Now().Format("2006/01/02 15:04:05"), req.Token)

	res, err := db.Exec(`
		UPDATE users
		SET email_verified = true,
		    verification_token = NULL,
		    verification_token_expires = NULL
		WHERE verification_token = $1 AND verification_token_expires > NOW()
	`, req.Token)

	if err != nil {
		log.Printf("%s ❌ Ошибка при обновлении email_verified в БД: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при подтверждении"})
		return
	}

	rows, err := res.RowsAffected()
	if err != nil {
		log.Printf("%s ⚠️ Ошибка при получении количества затронутых строк: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
	} else {
		log.Printf("%s ✅ Обновлено строк: %d\n", time.Now().Format("2006/01/02 15:04:05"), rows)
	}

	if rows == 0 {
		log.Printf("%s ⚠️ Токен недействителен или срок действия истёк\n", time.Now().Format("2006/01/02 15:04:05"))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный или просроченный токен"})
		return
	}

	log.Printf("%s 📬 Email подтверждён успешно\n", time.Now().Format("2006/01/02 15:04:05"))
	c.JSON(http.StatusOK, gin.H{"message": "Email успешно подтверждён"})
}

func verifyEmailHandler(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		log.Printf("%s ❌ GET /verify - токен не передан\n", time.Now().Format("2006/01/02 15:04:05"))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Токен не указан"})
		return
	}

	log.Printf("%s 🔍 GET /verify - получен токен: %s\n", time.Now().Format("2006/01/02 15:04:05"), token)

	var userID int
	var expiresAt time.Time
	err := db.QueryRow(`
		SELECT id, verification_token_expires 
		FROM users 
		WHERE verification_token = $1
	`, token).Scan(&userID, &expiresAt)

	if err != nil {
		log.Printf("%s ❌ Ошибка поиска токена в БД: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный или истёкший токен"})
		return
	}

	log.Printf("%s ✅ Найден пользователь с ID %d, токен действует до %v\n", time.Now().Format("2006/01/02 15:04:05"), userID, expiresAt)

	if time.Now().After(expiresAt) {
		log.Printf("%s ⏰ Токен истёк\n", time.Now().Format("2006/01/02 15:04:05"))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Срок действия токена истёк"})
		return
	}

	res, err := db.Exec(`
		UPDATE users 
		SET email_verified = true, verification_token = NULL, verification_token_expires = NULL 
		WHERE id = $1
	`, userID)

	if err != nil {
		log.Printf("%s ❌ Ошибка при обновлении email_verified: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка подтверждения"})
		return
	}

	rows, err := res.RowsAffected()
	if err != nil {
		log.Printf("%s ⚠️ Ошибка при получении количества обновлённых строк: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
	} else {
		log.Printf("%s 🔄 Обновлено строк: %d\n", time.Now().Format("2006/01/02 15:04:05"), rows)
	}

	if rows == 0 {
		log.Printf("%s ⚠️ Ни одна строка не обновлена — возможно, пользователь уже подтверждён\n", time.Now().Format("2006/01/02 15:04:05"))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ни одна строка не обновлена"})
		return
	}

	log.Printf("%s 📧 Email пользователя ID %d успешно подтверждён\n", time.Now().Format("2006/01/02 15:04:05"), userID)
	c.JSON(http.StatusOK, gin.H{"message": "Email успешно подтверждён"})
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || len(authHeader) <= len("Bearer ") {
			log.Printf("%s ❌ Отсутствует или пустой заголовок Authorization\n", time.Now().Format("2006/01/02 15:04:05"))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Токен не предоставлен"})
			return
		}

		tokenStr := authHeader[len("Bearer "):]
		log.Printf("%s 🔐 Получен токен: %s\n", time.Now().Format("2006/01/02 15:04:05"), tokenStr)

		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})
		if err != nil || !token.Valid {
			log.Printf("%s ❌ Неверный токен: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Неверный токен"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			log.Printf("%s ❌ Ошибка при чтении claims из токена\n", time.Now().Format("2006/01/02 15:04:05"))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Ошибка при чтении токена"})
			return
		}

		userIDFloat, ok := claims["user_id"].(float64)
		if !ok {
			log.Printf("%s ❌ Неверный формат user_id в токене\n", time.Now().Format("2006/01/02 15:04:05"))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Неверный формат user_id"})
			return
		}
		userID := int(userIDFloat)

		session, err := getSessionByToken(tokenStr)
		if err != nil {
			log.Printf("%s ❌ Ошибка получения сессии по токену: %v\n", time.Now().Format("2006/01/02 15:04:05"), err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Сессия недействительна"})
			return
		}
		if session.Revoked {
			log.Printf("%s ❌ Сессия с токеном %s была отозвана\n", time.Now().Format("2006/01/02 15:04:05"), tokenStr)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Сессия недействительна"})
			return
		}
		if session.ExpiresAt.Before(time.Now()) {
			log.Printf("%s ❌ Сессия с токеном %s истекла в %v\n", time.Now().Format("2006/01/02 15:04:05"), tokenStr, session.ExpiresAt)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Сессия истекла"})
			return
		}

		// 🔒 Проверка блокировки пользователя
		user, err := getUserByID(userID)
		if err != nil {
			log.Printf("%s ❌ Ошибка при получении пользователя ID=%d: %v\n", time.Now().Format("2006/01/02 15:04:05"), userID, err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Пользователь не найден"})
			return
		}
		if user.IsBlocked {
			log.Printf("%s 🚫 Пользователь ID=%d заблокирован\n", time.Now().Format("2006/01/02 15:04:05"), userID)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Пользователь заблокирован"})
			return
		}

		log.Printf("%s ✅ Успешная аутентификация пользователя с ID %d\n", time.Now().Format("2006/01/02 15:04:05"), userID)
		c.Set("user_id", userID)
		c.Next()
	}
}

func MeHandler(c *gin.Context) {
	userID := c.GetInt("user_id")

	user, err := getUserByID(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Пользователь не найден"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         user.ID,
		"first_name": user.FirstName,
		"last_name":  user.LastName,
		"email":      user.Email,
	})
}

type Session struct {
	ID        int
	UserID    int
	CreatedAt time.Time
	ExpiresAt time.Time
	Revoked   bool
	JWTToken  string
}

func getSessionByToken(token string) (*Session, error) {
	var session Session
	err := db.QueryRow(`
		SELECT id, user_id, created_at, expires_at, revoked, jwt_token
		FROM sessions
		WHERE jwt_token = $1
	`, token).Scan(&session.ID, &session.UserID, &session.CreatedAt, &session.ExpiresAt, &session.Revoked, &session.JWTToken)

	if err != nil {
		return nil, err
	}

	return &session, nil
}

func logoutHandler(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	tokenStr := authHeader[len("Bearer "):]

	_, err := db.Exec(`UPDATE sessions SET revoked = true WHERE jwt_token = $1`, tokenStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось завершить сессию"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Выход выполнен успешно"})
}

func checkAuthHandler(c *gin.Context) {
	userID := c.GetInt("user_id")
	user, err := getUserByID(userID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "unauthorized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "authorized",
		"user": gin.H{
			"id":       user.ID,
			"username": user.FirstName + " " + user.LastName,
			"email":    user.Email,
		},
	})
}

func generateTokens(userID int) (string, string, error) {
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(2 * time.Hour).Unix(),
	})
	accessTokenStr, err := accessToken.SignedString(jwtSecret)
	if err != nil {
		return "", "", err
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(7 * 24 * time.Hour).Unix(), // живёт дольше
	})
	refreshTokenStr, err := refreshToken.SignedString(jwtSecret)
	if err != nil {
		return "", "", err
	}

	return accessTokenStr, refreshTokenStr, nil
}
func refreshHandler(c *gin.Context) {
	refreshTokenStr, err := c.Cookie("refresh_token")
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Refresh токен не найден"})
		return
	}

	token, err := jwt.Parse(refreshTokenStr, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})
	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный refresh токен"})
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Ошибка при чтении токена"})
		return
	}

	userIDFloat, ok := claims["user_id"].(float64)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный user_id"})
		return
	}

	userID := int(userIDFloat)
	// Генерируем новый access_token
	newAccess, _, err := generateTokens(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при создании access токена"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token": newAccess,
	})
}

type InviteRequest struct {
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	Email          string `json:"email"`
	Role           string `json:"role"`
	OrganizationID int    `json:"organization_id"`
}

func inviteHandler(c *gin.Context) {
	var req InviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Ошибка разбора JSON: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}
	log.Printf("Получен запрос на приглашение: %+v\n", req)

	if existingUser, _ := getUserByEmail(req.Email); existingUser != nil {
		log.Printf("Пользователь с email %s уже существует\n", req.Email)
		c.JSON(http.StatusConflict, gin.H{"error": "Пользователь уже существует"})
		return
	}

	inviterID := c.GetInt("user_id")
	if inviterID == 0 {
		log.Printf("inviterID == 0, пользователь не авторизован или id не установлен в контекст\n")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Пользователь не авторизован"})
		return
	}

	inviter, err := getUserByID(inviterID)
	if err != nil {
		log.Printf("Ошибка получения пользователя по ID %d: %v\n", inviterID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при получении пользователя"})
		return
	}
	if inviter.OrganizationID == 0 {
		log.Printf("Организация не найдена для пользователя ID=%d\n", inviterID)
		c.JSON(http.StatusForbidden, gin.H{"error": "Организация не найдена для текущего пользователя"})
		return
	}

	// 👉 Получаем или создаём роль по имени
	roleID, err := getOrCreateRoleID(req.Role)
	if err != nil {
		log.Printf("Ошибка при получении ID роли %s: %v\n", req.Role, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при обработке роли"})
		return
	}

	// 🔑 Генерируем токен
	token := generateToken()
	expires := time.Now().Add(24 * time.Hour)
	log.Printf("Создан токен приглашения: %s, срок действия до: %s\n", token, expires.Format(time.RFC3339))

	// ✅ Создаём пользователя
	err = createUserWithVerification(
		req.FirstName,
		req.LastName,
		req.Email,
		"", // без пароля
		token,
		expires,
		inviter.OrganizationID,
		false, // is_owner = false
		roleID,
	)
	if err != nil {
		log.Printf("Ошибка при создании пользователя: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при создании пользователя"})
		return
	}
	log.Printf("Пользователь с email %s создан и приглашение сохранено в базе\n", req.Email)

	err = sendInvitationEmail(req.Email, token)
	if err != nil {
		log.Printf("Ошибка при отправке письма: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось отправить письмо"})
		return
	}
	log.Printf("Приглашение успешно отправлено на email %s\n", req.Email)

	c.JSON(http.StatusOK, gin.H{"message": "Приглашение отправлено"})
}

func sendInvitationEmail(toEmail string, token string) error {
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT")
	smtpUser := os.Getenv("SMTP_USER")
	smtpPass := os.Getenv("SMTP_PASS")
	frontendURL := os.Getenv("FRONTEND_URL")

	// Обновлённая ссылка на установку пароля
	inviteLink := frontendURL + "/set-password?token=" + token

	subject := "Приглашение в DocBuilder"
	body := "Здравствуйте!\n\nВы приглашены в DocBuilder.\nПерейдите по ссылке, чтобы завершить регистрацию:\n" + inviteLink

	msg := "From: " + smtpUser + "\n" +
		"To: " + toEmail + "\n" +
		"Subject: " + subject + "\n\n" + body

	auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)
	return smtp.SendMail(smtpHost+":"+smtpPort, auth, smtpUser, []string{toEmail}, []byte(msg))
}

type SetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func setPasswordHandler(c *gin.Context) {
	var req SetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	user, err := getUserByToken(req.Token)
	if err != nil || user == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Недействительный или просроченный токен"})
		return
	}

	// 🛠️ Обрабатываем sql.NullTime корректно
	if !user.VerificationTokenExpires.Valid || time.Now().After(user.VerificationTokenExpires.Time) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Срок действия токена истёк"})
		return
	}

	hashedPassword, err := hashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при хешировании пароля"})
		return
	}

	// ✅ Обновляем пользователя
	_, err = db.Exec(`
		UPDATE users 
		SET password_hash = $1, email_verified = TRUE, verification_token = '', verification_token_expires = NULL 
		WHERE id = $2
	`, hashedPassword, user.ID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при обновлении пользователя"})
		return
	}

	// ✅ Генерируем токены
	accessToken, refreshToken, err := generateTokens(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при создании токенов"})
		return
	}

	// ✅ Создаём сессию
	err = createSession(user.ID, accessToken, time.Now().Add(15*time.Minute))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при сохранении сессии"})
		return
	}

	// ✅ Устанавливаем refresh-токен в куки
	c.SetCookie("refresh_token", refreshToken, 7*24*60*60, "/", "localhost", false, true)

	// ✅ Отправляем клиенту access-токен и user_id
	c.JSON(http.StatusOK, gin.H{
		"message": "Пароль успешно установлен",
		"token":   accessToken,
		"user_id": user.ID,
	})
}

func getUserByToken(token string) (*User, error) {
	row := db.QueryRow(`
		SELECT id, email, password_hash, first_name, last_name, verification_token_expires, organization_id
		FROM users
		WHERE verification_token = $1
	`, token)

	var user User
	if err := row.Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.FirstName,
		&user.LastName,
		&user.VerificationTokenExpires,
		&user.OrganizationID,
	); err != nil {
		return nil, err
	}

	return &user, nil
}

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func createOrganization(name string) (int, error) {
	var orgID int
	err := db.QueryRow(`
		INSERT INTO organizations (name) 
		VALUES ($1) 
		RETURNING id
	`, name).Scan(&orgID)
	return orgID, err
}

func getInvitedUsersHandler(c *gin.Context) {
	inviterID := c.GetInt("user_id")
	if inviterID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Не авторизован"})
		return
	}

	inviter, err := getUserByID(inviterID)
	if err != nil || inviter.OrganizationID == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения пригласителя"})
		return
	}

	users, err := getUsersByOrganizationID(inviter.OrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения пользователей"})
		return
	}

	// 🎯 Формируем результат с нужными полями
	var result []gin.H
	for _, u := range users {
		result = append(result, gin.H{
			"ID":            u.ID,
			"FirstName":     u.FirstName,
			"LastName":      u.LastName,
			"Email":         u.Email,
			"EmailVerified": u.EmailVerified,
			"IsOwner":       u.IsOwner,
			"Role":          u.RoleName,
			"IsBlocked":     u.IsBlocked, // ✅ добавьте эту строку
		})
	}

	c.JSON(http.StatusOK, result)
}

type AssignRoleRequest struct {
	UserIDs []int  `json:"user_ids"` // список пользователей
	Role    string `json:"role"`     // новая роль: "Менеджер", "Администратор", "Владелец" и т.д.
}

type Role struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func assignRoleHandler(c *gin.Context) {
	var req AssignRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("❌ Ошибка разбора JSON: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	// 🔐 Авторизованный пользователь
	adminID := c.GetInt("user_id")
	admin, err := getUserByID(adminID)
	if err != nil {
		log.Printf("❌ Ошибка получения администратора ID=%d: %v\n", adminID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка авторизации"})
		return
	}

	// 🔒 Только владелец (1) или админ (2) имеют право менять роли
	if admin.RoleID != 1 && admin.RoleID != 2 {
		log.Printf("⛔ Недостаточно прав: user_id=%d, role_id=%d\n", admin.ID, admin.RoleID)
		c.JSON(http.StatusForbidden, gin.H{"error": "Недостаточно прав для назначения ролей"})
		return
	}

	// ⚠️ Проверка: нельзя назначить пустую роль
	if req.Role == "" || len(req.UserIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Не указана роль или список пользователей"})
		return
	}

	// ✅ Создаём роль при необходимости
	roleID, err := ensureRoleExists(req.Role)
	if err != nil {
		log.Printf("❌ Ошибка при проверке/создании роли %s: %v\n", req.Role, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при обработке роли"})
		return
	}

	// 🧠 Только один владелец
	if req.Role == "Владелец" {
		existingOwner, err := findUserByRole("Владелец")
		if err == nil && existingOwner != nil {
			for _, userID := range req.UserIDs {
				if existingOwner.ID != userID {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Роль 'Владелец' уже назначена другому пользователю"})
					return
				}
			}
		}
	}

	// ✅ Назначаем роль всем пользователям
	for _, userID := range req.UserIDs {
		err := updateUserRole(userID, req.Role)
		if err != nil {
			log.Printf("❌ Ошибка при обновлении роли для пользователя ID=%d: %v\n", userID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить роль пользователя"})
			return
		}
	}

	log.Printf("✅ Назначена роль '%s' (ID=%d) пользователям: %v\n", req.Role, roleID, req.UserIDs)
	c.JSON(http.StatusOK, gin.H{"message": "Роль успешно обновлена"})
}

func getAllUsersHandler(c *gin.Context) {
	users, err := getAllUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось получить пользователей"})
		return
	}

	var result []gin.H
	for _, u := range users {
		result = append(result, gin.H{
			"id":       u.ID,
			"name":     fmt.Sprintf("%s %s", u.FirstName, u.LastName),
			"position": u.RoleName, // ✅ теперь мы используем название роли
		})
	}

	c.JSON(http.StatusOK, result)
}

func findUserByRole(roleName string) (*User, error) {
	row := db.QueryRow(`
		SELECT u.id, u.email, u.first_name, u.last_name, u.role_id, r.name
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE r.name = $1
		LIMIT 1
	`, roleName)

	var user User
	err := row.Scan(&user.ID, &user.Email, &user.FirstName, &user.LastName, &user.RoleID, &user.RoleName)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func getAllRolesHandler(c *gin.Context) {
	rows, err := db.Query("SELECT id, name FROM roles")
	if err != nil {
		log.Printf("Ошибка при получении ролей: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при получении ролей"})
		return
	}
	defer rows.Close()

	var roles []Role
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Name); err != nil {
			log.Printf("Ошибка при чтении строки роли: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при чтении данных роли"})
			return
		}
		roles = append(roles, role)
	}

	c.JSON(http.StatusOK, roles)
}

func ensureRoleExists(roleName string) (int, error) {
	var roleID int
	err := db.QueryRow("SELECT id FROM roles WHERE name = $1", roleName).Scan(&roleID)
	if err == sql.ErrNoRows {
		// роли нет — создаём
		err = db.QueryRow("INSERT INTO roles (name) VALUES ($1) RETURNING id", roleName).Scan(&roleID)
	}
	return roleID, err
}

func renameRoleHandler(c *gin.Context) {
	var req RenameRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Ошибка разбора JSON: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	// Проверка существования старой роли
	var roleID int
	err := db.QueryRow("SELECT id FROM roles WHERE name = $1", req.OldName).Scan(&roleID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Роль не найдена"})
		return
	} else if err != nil {
		log.Printf("Ошибка при поиске роли: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при проверке роли"})
		return
	}

	// Обновление имени роли
	_, err = db.Exec("UPDATE roles SET name = $1 WHERE id = $2", req.NewName, roleID)
	if err != nil {
		log.Printf("Ошибка при переименовании роли: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось переименовать роль"})
		return
	}

	log.Printf("✅ Роль '%s' переименована в '%s'\n", req.OldName, req.NewName)
	c.JSON(http.StatusOK, gin.H{"message": "Роль успешно переименована"})
}

func deleteRoleHandler(c *gin.Context) {
	var req DeleteRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Ошибка разбора JSON: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	// Проверяем, существует ли роль
	var roleID int
	err := db.QueryRow("SELECT id FROM roles WHERE name = $1", req.Name).Scan(&roleID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Роль не найдена"})
		return
	} else if err != nil {
		log.Printf("Ошибка при поиске роли: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при проверке роли"})
		return
	}

	// Проверка: никто не должен использовать эту роль
	var userCount int
	err = db.QueryRow("SELECT COUNT(*) FROM users WHERE role_id = $1", roleID).Scan(&userCount)
	if err != nil {
		log.Printf("Ошибка при проверке использования роли: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось проверить роль"})
		return
	}
	if userCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нельзя удалить роль, которая назначена пользователям"})
		return
	}

	// Удаляем роль
	_, err = db.Exec("DELETE FROM roles WHERE id = $1", roleID)
	if err != nil {
		log.Printf("Ошибка при удалении роли: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось удалить роль"})
		return
	}

	log.Printf("🗑️ Роль '%s' удалена\n", req.Name)
	c.JSON(http.StatusOK, gin.H{"message": "Роль успешно удалена"})
}

func blockUserHandler(c *gin.Context) {
	adminID := c.GetInt("user_id")
	admin, err := getUserByID(adminID)
	if err != nil {
		log.Printf("❌ Ошибка получения администратора ID=%d: %v\n", adminID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка авторизации"})
		return
	}

	log.Printf("👤 Попытка блокировки: AdminID=%d, RoleID=%d, RoleName=%s, IsOwner=%v\n",
		admin.ID, admin.RoleID, admin.RoleName, admin.IsOwner)

	// ✅ Только Владелец (1) и Администратор (2)
	if admin.RoleID != 1 && admin.RoleID != 2 {
		log.Printf("⛔ Недостаточно прав: RoleID=%d\n", admin.RoleID)
		c.JSON(http.StatusForbidden, gin.H{"error": "Недостаточно прав"})
		return
	}

	userIDStr := c.Param("id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		log.Printf("❌ Неверный ID пользователя: %s\n", userIDStr)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный ID"})
		return
	}

	err = setUserBlockedStatus(userID, true)
	if err != nil {
		log.Printf("❌ Ошибка при блокировке пользователя ID=%d: %v\n", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось заблокировать пользователя"})
		return
	}

	log.Printf("✅ Пользователь ID=%d успешно заблокирован\n", userID)
	c.JSON(http.StatusOK, gin.H{"message": "Пользователь заблокирован"})
}

func deleteUserHandler(c *gin.Context) {
	adminID := c.GetInt("user_id")
	admin, err := getUserByID(adminID)
	if err != nil || (!admin.IsOwner && admin.RoleName != "Администратор") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Недостаточно прав"})
		return
	}

	userIDStr := c.Param("id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный ID"})
		return
	}

	// нельзя удалить самого себя
	if adminID == userID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нельзя удалить самого себя"})
		return
	}

	_, err = db.Exec("DELETE FROM users WHERE id = $1", userID)
	if err != nil {
		log.Printf("❌ Ошибка при удалении пользователя ID=%d: %v\n", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось удалить пользователя"})
		return
	}

	log.Printf("🗑️ Пользователь ID=%d успешно удалён\n", userID)
	c.JSON(http.StatusOK, gin.H{"message": "Пользователь удалён"})
}

func unblockUserHandler(c *gin.Context) {
	adminID := c.GetInt("user_id")
	admin, err := getUserByID(adminID)
	if err != nil {
		log.Printf("❌ Ошибка получения администратора ID=%d: %v\n", adminID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка авторизации"})
		return
	}

	// 🐞 Логируем данные о текущем пользователе
	log.Printf("👤 Попытка разблокировки: AdminID=%d, RoleID=%d, RoleName=%s, IsOwner=%v\n",
		admin.ID, admin.RoleID, admin.RoleName, admin.IsOwner)

	// ✅ Только Владелец (1) и Администратор (2)
	if admin.RoleID != 1 && admin.RoleID != 2 {
		log.Printf("⛔ Недостаточно прав: RoleID=%d\n", admin.RoleID)
		c.JSON(http.StatusForbidden, gin.H{"error": "Недостаточно прав"})
		return
	}

	userIDParam := c.Param("id")
	userID, err := strconv.Atoi(userIDParam)
	if err != nil || userID == 0 {
		log.Printf("❌ Некорректный ID пользователя: %s\n", userIDParam)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный ID пользователя"})
		return
	}

	_, err = db.Exec("UPDATE users SET is_blocked = FALSE WHERE id = $1", userID)
	if err != nil {
		log.Printf("❌ Ошибка при разблокировке пользователя ID=%d: %v\n", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при разблокировке"})
		return
	}

	log.Printf("✅ Пользователь ID=%d успешно разблокирован\n", userID)
	c.JSON(http.StatusOK, gin.H{"message": "Пользователь разблокирован"})
}
