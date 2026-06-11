package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"text/template"

	"github.com/gin-gonic/gin"

	"github.com/dujianqiang/broodvm/internal/api"
	"github.com/dujianqiang/broodvm/internal/api/handler"
	"github.com/dujianqiang/broodvm/internal/config"
	lv "github.com/dujianqiang/broodvm/internal/libvirt"
	"github.com/dujianqiang/broodvm/internal/service"
	"github.com/dujianqiang/broodvm/internal/store"
)

//go:embed templates web
var embeddedFS embed.FS

func main() {
	// 1. 加载配置
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 2. 确保种子镜像存在
	seedImage, err := config.EnsureSeedImage(cfg)
	if err != nil {
		log.Fatalf("seed image: %v", err)
	}
	log.Printf("seed image: %s", seedImage)

	// 3. 打开数据库
	db, err := store.Open("broodvm.db")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// 4. 初始化 store
	vmStore := store.NewVMStore(db)
	taskStore := store.NewTaskStore(db)

	// 5. 连接 libvirt
	virtClient, err := lv.NewLibvirtClient("qemu:///system")
	if err != nil {
		log.Fatalf("libvirt: %v", err)
	}

	// 6. 加载 VM XML 模板
	tmplContent, err := embeddedFS.ReadFile("templates/vm-domain.xml.tmpl")
	if err != nil {
		log.Fatalf("read template: %v", err)
	}
	vmTmpl, err := template.New("vm-domain").Parse(string(tmplContent))
	if err != nil {
		log.Fatalf("parse template: %v", err)
	}

	// 7. 初始化 service
	sessions := service.NewSessionStore()
	vmSvc := service.NewVMService(cfg, vmStore, taskStore, virtClient, seedImage)
	vmSvc.SetTemplate(vmTmpl)

	// 8. 初始化 handler
	authH := handler.NewAuthHandler(cfg, sessions)
	vmH := handler.NewVMHandler(vmSvc)
	taskH := handler.NewTaskHandler(taskStore)

	// 9. 设置路由
	r := api.NewRouter(sessions, authH, vmH, taskH)

	// 10. 挂载前端静态文件（SPA fallback）
	subFS, _ := fs.Sub(embeddedFS, "web")
	fileServer := http.FileServer(http.FS(subFS))
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		// 若静态文件存在则直接服务，否则回退到 index.html（SPA 路由）
		filePath := strings.TrimPrefix(c.Request.URL.Path, "/")
		if filePath != "" {
			if f, err := subFS.Open(filePath); err == nil {
				f.Close()
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})

	// 11. 启动
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("BroodVM listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("run: %v", err)
	}
}
