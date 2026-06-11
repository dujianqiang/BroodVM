package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dujianqiang/broodvm/internal/service"
)

func Auth(sessions *service.SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie("session")
		if err != nil || !sessions.Validate(token) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			c.Abort()
			return
		}
		c.Next()
	}
}
