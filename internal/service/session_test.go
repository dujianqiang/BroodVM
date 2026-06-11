package service_test

import (
	"testing"

	"github.com/dujianqiang/broodvm/internal/service"
)

func TestSessionStore(t *testing.T) {
	s := service.NewSessionStore()

	token := s.Create("admin")
	if token == "" {
		t.Fatal("token should not be empty")
	}

	if !s.Validate(token) {
		t.Error("valid token should validate")
	}
	if s.Validate("bad-token") {
		t.Error("bad token should not validate")
	}

	s.Delete(token)
	if s.Validate(token) {
		t.Error("deleted token should not validate")
	}
}
