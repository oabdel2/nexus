package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nexus-gateway/nexus/internal/config"
	"github.com/nexus-gateway/nexus/internal/gateway"
)

var (
	version = "0.1.0"
	banner  = `
 _   _                      
| \ | | _____  ___   _ ___  
|  \| |/ _ \ \/ / | | / __| 
| |\  |  __/>  <| |_| \__ \ 
|_| \_|\___/_/\_\\__,_|___/ 
                             
Agentic-First Inference Optimization Gateway v%s
`
)

func main() {
	configPath := flag.String("config", "configs/nexus.yaml", "path to configuration file")
	port := flag.Int("port", 0, "override server port")
	logLevel := flag.String("log-level", "", "log level (debug, info, warn, error)")
	showVersion := flag.Bool("version", false, "show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("nexus v%s\n", version)
		os.Exit(0)
	}

	fmt.Printf(banner, version)

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		// Use defaults if config file not found
		slog.Warn("config file not found, using defaults", "path", *configPath, "error", err)
		cfg = config.DefaultConfig()
	}

	// Apply CLI overrides
	if *port > 0 {
		cfg.Server.Port = *port
	}

	// Setup logger
	level := slog.LevelInfo
	logLvl := cfg.Telemetry.LogLevel
	if *logLevel != "" {
		logLvl = *logLevel
	}
	switch logLvl {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	var handler slog.Handler
	if cfg.Telemetry.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	logger := slog.New(handler)

	// Expand env vars in API keys
	for i := range cfg.Providers {
		cfg.Providers[i].APIKey = os.ExpandEnv(cfg.Providers[i].APIKey)
	}

	// Create and start server
	srv := gateway.New(cfg, logger)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("shutting down...")
		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
		cancel()
	}()

	if err := srv.Start(ctx); err != nil && err.Error() != "http: Server closed" {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}

	logger.Info("nexus gateway stopped")
}
