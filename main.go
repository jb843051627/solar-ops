package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"solar-ops/internal/api"
	"solar-ops/internal/service"
	"solar-ops/internal/store"
)

func main() {
	dbPath := os.Getenv("SOLAR_OPS_DB")
	if dbPath == "" {
		dbPath = "solar-ops.db"
	}

	// 初始化存储
	s, err := store.NewStore(dbPath)
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	defer s.Close()

	// 创建服务
	svc := service.NewMonitoringService(s)

	// 启动后台监控
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.StartBackgroundMonitor(ctx)

	// 创建 HTTP handler
	handler := api.NewHandler(svc)

	// 注册站点创建路由（直接挂在 handler 的 mux 上）
	mux := http.NewServeMux()
	mux.Handle("/api/", handler)
	mux.HandleFunc("/api/sites", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handler.RegisterSite(w, r)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	port := os.Getenv("SOLAR_OPS_PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 优雅关闭
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	log.Printf("solar-ops listening on :%s (db: %s)", port, dbPath)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
	log.Println("server stopped")
}
