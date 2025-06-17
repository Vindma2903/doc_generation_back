package auth

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"net/http"
)

var jwtSecret = []byte("super_secret_key") // ❗ желательно хранить в .env

func RegisterRoutes(r *gin.Engine) {
	r.POST("/login", loginHandler)
	r.POST("/register", registerHandler)
	r.POST("/refresh", refreshHandler)

	// ✅ защищённые маршруты через группу
	authGroup := r.Group("/")
	authGroup.Use(AuthMiddleware())
	authGroup.GET("/me", MeHandler)
	authGroup.GET("/auth/check", checkAuthHandler)

}

// ----------------- LOGIN -----------------

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func loginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	user, err := getUserByEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Пользователь не найден"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный пароль"})
		return
	}

	// 🔐 Генерируем access и refresh токены
	accessToken, refreshToken, err := generateTokens(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при создании токенов"})
		return
	}

	// 💾 Сохраняем access-токен в сессии
	err = createSession(user.ID, accessToken, time.Now().Add(15*time.Minute))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при сохранении сессии"})
		return
	}

	// 🍪 Устанавливаем refresh-токен в HttpOnly cookie
	c.SetCookie("refresh_token", refreshToken, 7*24*60*60, "/", "localhost", false, true)
	// Параметры:
	// 7 дней, path "/", домен "localhost", secure=false (true на HTTPS), httpOnly=true

	// 📦 Отправляем access-токен и user_id клиенту
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
}

func registerHandler(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный JSON"})
		return
	}

	if existingUser, _ := getUserByEmail(req.Email); existingUser != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Пользователь с таким email уже существует"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при хешировании пароля"})
		return
	}

	err = createUser(req.FirstName, req.LastName, req.Email, string(hashedPassword))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при создании пользователя"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Пользователь успешно зарегистрирован"})
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || len(authHeader) <= len("Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Токен не предоставлен"})
			return
		}

		tokenStr := authHeader[len("Bearer "):]
		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Неверный токен"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Ошибка при чтении токена"})
			return
		}

		userIDFloat, ok := claims["user_id"].(float64)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Неверный формат user_id"})
			return
		}

		// ✅ Проверка сессии
		session, err := getSessionByToken(tokenStr)
		if err != nil || session.Revoked || session.ExpiresAt.Before(time.Now()) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Сессия недействительна"})
			return
		}

		c.Set("user_id", int(userIDFloat))
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
		"exp":     time.Now().Add(15 * time.Minute).Unix(), // короткоживущий
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
