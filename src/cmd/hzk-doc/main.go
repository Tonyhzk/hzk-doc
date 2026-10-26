package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hzk-doc/internal/admin"
	"hzk-doc/internal/catalog"
	"hzk-doc/internal/server"
	"hzk-doc/internal/webui"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		slog.Error("启动失败", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseOptions(args, os.Stderr)
	if err != nil {
		return err
	}
	if cfg.showVersion {
		fmt.Println(version)
		return nil
	}
	password := os.Getenv("HZK_DOC_ADMIN_PASSWORD")
	if password != "" && len([]rune(password)) < 12 {
		return errors.New("HZK_DOC_ADMIN_PASSWORD 至少需要 12 个字符")
	}
	store, err := catalog.Open(cfg.docs)
	if err != nil {
		return err
	}
	defer store.Close()
	if cfg.migrateIDs {
		count, err := store.MigrateFilenames()
		if err != nil {
			return err
		}
		fmt.Printf("已登记文档 ID，更新 %d 个文件名。\n", count)
		return nil
	}
	assets, err := webui.Files()
	if err != nil {
		return err
	}
	reader, err := server.New(server.Config{
		Catalog: store, DocsDir: cfg.docs, SiteName: cfg.name, Version: version,
		AdminEnabled: password != "", RefreshInterval: cfg.refresh,
	}, assets)
	if err != nil {
		return err
	}
	defer reader.Close()
	manager, err := admin.New(admin.Config{
		Catalog: store, DocsDir: cfg.docs, Password: password,
		OnChange: reader.Refresh, Preview: reader.PreviewMarkdown,
	})
	if err != nil {
		return err
	}
	defer manager.Close()
	mux := http.NewServeMux()
	mux.Handle("/api/admin/", manager)
	mux.Handle("/", reader)
	listener, err := net.Listen("tcp", cfg.listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	slog.Info("文档站已启动", "listen", listener.Addr().String(), "version", version, "admin", password != "")
	return serve(listener, mux)
}

func serve(listener net.Listener, handler http.Handler) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	httpServer := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 2 * time.Minute, WriteTimeout: 5 * time.Minute,
		IdleTimeout: 90 * time.Second,
	}
	result := make(chan error, 1)
	go func() { result <- httpServer.Serve(listener) }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdown); err != nil {
			_ = httpServer.Close()
			return err
		}
		return nil
	}
}
