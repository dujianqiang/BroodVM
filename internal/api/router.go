package api

import (
	"github.com/gin-gonic/gin"

	"github.com/dujianqiang/broodvm/internal/api/handler"
	"github.com/dujianqiang/broodvm/internal/api/middleware"
	"github.com/dujianqiang/broodvm/internal/service"
)

func NewRouter(
	sessions *service.SessionStore,
	authH *handler.AuthHandler,
	vmH *handler.VMHandler,
	taskH *handler.TaskHandler,
) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	api := r.Group("/api")
	api.POST("/login", authH.Login)
	api.POST("/logout", authH.Logout)

	protected := api.Group("", middleware.Auth(sessions))
	protected.GET("/vms", vmH.List)
	protected.POST("/vms", vmH.Create)
	protected.GET("/vms/:id", vmH.Get)
	protected.DELETE("/vms/:id", vmH.Delete)
	protected.POST("/vms/:id/start", vmH.Start)
	protected.POST("/vms/:id/stop", vmH.Stop)
	protected.POST("/vms/:id/restart", vmH.Restart)
	protected.GET("/tasks/:task_id", taskH.Get)

	return r
}
