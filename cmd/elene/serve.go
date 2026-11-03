package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/nyxar77/elene/internal/adb"
	"github.com/nyxar77/elene/internal/dashboard"
)

func serve(ctx context.Context, adbPath, port string, stdout io.Writer) error {
	if port == "" {
		port = "18420"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("ELENE_PORT must be between 1 and 65535")
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	client, err := adb.New(adb.Config{ADBPath: adbPath, CommandTimeout: 5 * time.Minute})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return fmt.Errorf("open local dashboard: %w", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		_ = listener.Close()
		return err
	}
	dashboardServer, err := dashboard.New(ctx, client, filepath.Join(configDir, "eden", "config.json"))
	if err != nil {
		_ = listener.Close()
		return err
	}
	dashboardServer.StartBackgroundSync()
	server := &http.Server{Handler: dashboardServer.Handler(listener.Addr().String()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Minute, WriteTimeout: 5 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	defer dashboardServer.Close()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if server.Shutdown(shutdownCtx) != nil {
			_ = server.Close()
		}
	}()
	fmt.Fprintf(stdout, "Elene is running at http://%s\nPress Ctrl+C to stop.\n", listener.Addr())
	err = server.Serve(listener)
	stop()
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
