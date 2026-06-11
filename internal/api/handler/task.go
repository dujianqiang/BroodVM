package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dujianqiang/broodvm/internal/store"
)

type TaskHandler struct {
	taskStore *store.TaskStore
}

func NewTaskHandler(taskStore *store.TaskStore) *TaskHandler {
	return &TaskHandler{taskStore: taskStore}
}

func (h *TaskHandler) Get(c *gin.Context) {
	task, err := h.taskStore.Get(c.Param("task_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, task)
}
