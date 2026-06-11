package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dujianqiang/broodvm/internal/config"
	"github.com/dujianqiang/broodvm/internal/service"
)

type AuthHandler struct {
	cfg      *config.Config
	sessions *service.SessionStore
}

func NewAuthHandler(cfg *config.Config, sessions *service.SessionStore) *AuthHandler {
	return &AuthHandler{cfg: cfg, sessions: sessions}
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if req.Username != h.cfg.Auth.Username || req.Password != h.cfg.Auth.Password {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	token := h.sessions.Create(req.Username)
	c.SetCookie("session", token, 86400, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "登录成功"})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	token, _ := c.Cookie("session")
	if token != "" {
		h.sessions.Delete(token)
	}
	c.SetCookie("session", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "已登出"})
}
