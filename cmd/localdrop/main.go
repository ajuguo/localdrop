package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"localdrop/internal/localdrop"
)

func main() {
	logger := log.New(os.Stdout, "[localdrop] ", log.LstdFlags|log.Lmsgprefix)

	cfg, err := localdrop.LoadConfig()
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}

	app, err := localdrop.NewApp(cfg, logger)
	if err != nil {
		logger.Fatalf("create app: %v", err)
	}
	defer app.Close()

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Printf("listening on http://%s", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Printf("shutdown: %v", err)
	}
}
