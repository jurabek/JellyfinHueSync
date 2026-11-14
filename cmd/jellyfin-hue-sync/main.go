// Command jellyfin-hue-sync runs the Jellyfin Hue Sync web app.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jurabek/JellyfinHueSync/internal/app"
	"github.com/jurabek/JellyfinHueSync/internal/config"
)

func main() {
	configDir := envOr("CONFIG_DIR", "config")
	port := envOr("PORT", "8091")

	store := config.NewStore(configDir)
	handler := app.New(store).Handler()

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("Jellyfin Hue Sync listening on %s (config: %s)", srv.Addr, configDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
