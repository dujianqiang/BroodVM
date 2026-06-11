package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/dujianqiang/broodvm/internal/api/handler"
	"github.com/dujianqiang/broodvm/internal/api/middleware"
	"github.com/dujianqiang/broodvm/internal/config"
	"github.com/dujianqiang/broodvm/internal/service"
)

func init() { gin.SetMode(gin.TestMode) }

func newAuthRouter() (*gin.Engine, *service.SessionStore) {
	cfg := &config.Config{}
	cfg.Auth.Username = "admin"
	cfg.Auth.Password = "secret"
	sessions := service.NewSessionStore()
	h := handler.NewAuthHandler(cfg, sessions)
	r := gin.New()
	r.POST("/api/login", h.Login)
	r.POST("/api/logout", middleware.Auth(sessions), h.Logout)
	return r, sessions
}

func TestLogin_Success(t *testing.T) {
	r, _ := newAuthRouter()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == "session" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Error("session cookie not set")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	r, _ := newAuthRouter()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrong"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestMiddleware_Unauthorized(t *testing.T) {
	r, _ := newAuthRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}
