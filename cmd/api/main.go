package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kanatovs/taskflow-api/internal/config"
	"github.com/kanatovs/taskflow-api/internal/handler"
	platformcache "github.com/kanatovs/taskflow-api/internal/platform/cache"
	"github.com/kanatovs/taskflow-api/internal/platform/database"
	"github.com/kanatovs/taskflow-api/internal/platform/httpserver"
	"github.com/kanatovs/taskflow-api/internal/platform/token"
	repositorycache "github.com/kanatovs/taskflow-api/internal/repository/cache"
	"github.com/kanatovs/taskflow-api/internal/repository/postgres"
	"github.com/kanatovs/taskflow-api/internal/service"
)

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("Invalid configuration", "error", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := database.New(ctx, cfg.DatabaseURL, cfg.DatabaseTimeout)
	if err != nil {
		logger.Error("Database configuration failed", "error", err)
		return 1
	}
	defer pool.Close()
	users := postgres.NewUserRepository(pool, cfg.DatabaseTimeout)
	tokens, err := token.NewJWT(cfg.JWTSecret, cfg.JWTAccessTTL)
	if err != nil {
		logger.Error("JWT configuration failed", "error", err)
		return 1
	}
	auth := handler.NewAuthHandler(service.NewAuthService(users, tokens), tokens)
	projects := postgres.NewProjectRepository(pool, cfg.DatabaseTimeout)
	members := postgres.NewProjectMemberRepository(pool, cfg.DatabaseTimeout)
	var projectDetails service.ProjectDetailReader
	var taskDetails service.TaskDetailReader
	tasks := postgres.NewTaskRepository(pool, cfg.DatabaseTimeout)
	if cfg.Redis.Addr != "" {
		cache := platformcache.New(cfg.Redis)
		defer func() {
			if err := cache.Close(); err != nil {
				logger.Warn("Redis client close failed")
			}
		}()
		projectDetails = repositorycache.NewProject(projects, cache, cfg.Redis.TTL, logger)
		taskDetails = repositorycache.NewTask(tasks, cache, cfg.Redis.TTL, logger)
	}
	projectService := service.NewProjectService(projects, members, users, projectDetails)
	projectHandler := handler.NewProjectHandler(projectService, service.NewProjectMemberService(projectService, members))
	comments := postgres.NewCommentRepository(pool, cfg.DatabaseTimeout)
	commentHandler := handler.NewCommentHandler(service.NewCommentService(projectService, tasks, comments))
	taskHandler := handler.NewTaskHandler(service.NewTaskService(projectService, tasks, taskDetails), commentHandler)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler.NewRouter(cfg.HTTPRequestTimeout, logger, handler.NewReadiness(pool, cfg.DatabaseTimeout), auth, projectHandler, taskHandler),
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		logger.Error("HTTP listener failed", "error", err)
		return 1
	}
	if err := httpserver.Serve(ctx, server, listener, cfg.HTTPShutdownTimeout, logger); err != nil {
		logger.Error("HTTP server failed", "error", err)
		return 1
	}
	return 0
}
