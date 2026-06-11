package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dujianqiang/broodvm/internal/service"
	"github.com/dujianqiang/broodvm/internal/store"
)

type VMHandler struct {
	svc *service.VMService
}

func NewVMHandler(svc *service.VMService) *VMHandler {
	return &VMHandler{svc: svc}
}

func (h *VMHandler) List(c *gin.Context) {
	vms, err := h.svc.ListVMs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if vms == nil {
		vms = []*store.VM{}
	}
	c.JSON(http.StatusOK, vms)
}

func (h *VMHandler) Create(c *gin.Context) {
	var req service.CreateVMReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	taskID, err := h.svc.Create(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"task_id": taskID})
}

func (h *VMHandler) Get(c *gin.Context) {
	vm, err := h.svc.GetVM(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "VM 不存在"})
		return
	}
	c.JSON(http.StatusOK, vm)
}

func (h *VMHandler) Delete(c *gin.Context) {
	taskID, err := h.svc.Delete(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"task_id": taskID})
}

func (h *VMHandler) Start(c *gin.Context) {
	if err := h.svc.Start(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已启动"})
}

func (h *VMHandler) Stop(c *gin.Context) {
	if err := h.svc.Stop(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已关机"})
}

func (h *VMHandler) Restart(c *gin.Context) {
	if err := h.svc.Restart(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已重启"})
}
