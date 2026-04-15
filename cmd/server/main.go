// Package main is the entry point for the FUJU backend server.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fuju/backend/config"
	"github.com/fuju/backend/internal/handler"
	"github.com/fuju/backend/internal/middleware"
	"github.com/fuju/backend/internal/repository/inmemory"
	commentusecase "github.com/fuju/backend/internal/usecase/comment"
	imageusecase "github.com/fuju/backend/internal/usecase/image"
	postusecase "github.com/fuju/backend/internal/usecase/post"
	userusecase "github.com/fuju/backend/internal/usecase/user"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/logger"
	"github.com/fuju/backend/pkg/storage"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log := logger.New(cfg.LogLevel)
	ctx := context.Background()

	log.Info(ctx, "Starting FUJU Backend Server",
		"version", Version,
		"build_time", BuildTime,
		"environment", cfg.Environment,
	)

	// Initialize repositories (in-memory for now)
	userRepo := inmemory.NewUserRepository()
	postRepo := inmemory.NewPostRepository()
	commentRepo := inmemory.NewCommentRepository()

	// Initialize token manager
	tokenManager := auth.NewTokenManager(
		cfg.JWTSecret,
		time.Duration(cfg.JWTExpiration)*time.Second,
		time.Duration(cfg.JWTExpiration*2)*time.Second,
	)

	// Create HTTP mux
	mux := http.NewServeMux()

	// Initialize handlers
	healthHandler := handler.NewHealthHandler()

	// User handlers
	userGetUC := userusecase.NewGetUserUseCase(userRepo)
	userCreateUC := userusecase.NewCreateUserUseCase(userRepo)
	userUpdateUC := userusecase.NewUpdateUserUseCase(userRepo)
	userListUC := userusecase.NewListUsersUseCase(userRepo)
	userHandler := handler.NewUserHandler(userGetUC, userCreateUC, userUpdateUC, userListUC)

	// Post handlers
	postGetUC := postusecase.NewGetPostUseCase(postRepo)
	postCreateUC := postusecase.NewCreatePostUseCase(postRepo)
	postDeleteUC := postusecase.NewDeletePostUseCase(postRepo)
	postListUC := postusecase.NewListPostsUseCase(postRepo)
	postHandler := handler.NewPostHandler(postGetUC, postCreateUC, postDeleteUC, postListUC)

	// Comment handlers
	commentAddUC := commentusecase.NewAddCommentUseCase(commentRepo, postRepo)
	commentDeleteUC := commentusecase.NewDeleteCommentUseCase(commentRepo, postRepo)
	commentHandler := handler.NewCommentHandlerImpl(commentAddUC, commentDeleteUC)

	// Image handlers
	imageRepo := inmemory.NewImageRepository()
	r2Service, err := storage.NewR2Service()
	if err != nil {
		log.Warn(ctx, "Failed to initialize R2 service", err)
		r2Service = nil
	}
	var imageHandler *handler.ImageHandler
	if r2Service != nil {
		uploadImageUC := imageusecase.NewUploadImageUseCase(imageRepo, r2Service)
		getUserImagesUC := imageusecase.NewGetUserImagesUseCase(imageRepo)
		deleteImageUC := imageusecase.NewDeleteImageUseCase(imageRepo, r2Service)
		imageHandler = handler.NewImageHandler(uploadImageUC, getUserImagesUC, deleteImageUC)
	}

	// Register routes
	// Health
	mux.HandleFunc("GET /health", healthHandler.Health)

	// Users
	mux.HandleFunc("GET /users", userHandler.ListUsers)
	mux.HandleFunc("POST /users", middleware.AuthMiddleware(tokenManager)(
		http.HandlerFunc(userHandler.CreateUser),
	).ServeHTTP)
	mux.HandleFunc("GET /users/{id}", userHandler.GetUser)
	mux.HandleFunc("PUT /users/{id}", middleware.AuthMiddleware(tokenManager)(
		http.HandlerFunc(userHandler.UpdateUser),
	).ServeHTTP)

	// Posts
	mux.HandleFunc("GET /posts", postHandler.ListPosts)
	mux.HandleFunc("POST /posts", middleware.AuthMiddleware(tokenManager)(
		http.HandlerFunc(postHandler.CreatePost),
	).ServeHTTP)
	mux.HandleFunc("GET /posts/{id}", postHandler.GetPost)
	mux.HandleFunc("DELETE /posts/{id}", middleware.AuthMiddleware(tokenManager)(
		http.HandlerFunc(postHandler.DeletePost),
	).ServeHTTP)

	// Comments
	mux.HandleFunc("POST /posts/{id}/comments", middleware.AuthMiddleware(tokenManager)(
		http.HandlerFunc(commentHandler.AddComment),
	).ServeHTTP)
	mux.HandleFunc("DELETE /posts/{post_id}/comments/{comment_id}", middleware.AuthMiddleware(tokenManager)(
		http.HandlerFunc(commentHandler.DeleteComment),
	).ServeHTTP)

	// Images (if R2 is configured)
	if imageHandler != nil {
		mux.HandleFunc("POST /v1/images", middleware.AuthMiddleware(tokenManager)(
			http.HandlerFunc(imageHandler.UploadImage),
		).ServeHTTP)
		mux.HandleFunc("GET /v1/images", middleware.AuthMiddleware(tokenManager)(
			http.HandlerFunc(imageHandler.GetUserImages),
		).ServeHTTP)
		mux.HandleFunc("DELETE /v1/images/{id}", middleware.AuthMiddleware(tokenManager)(
			http.HandlerFunc(imageHandler.DeleteImage),
		).ServeHTTP)
	}

	// Apply global middleware
	var apiHandler http.Handler = mux
	apiHandler = middleware.CORSMiddleware()(apiHandler)
	apiHandler = middleware.RecoveryMiddleware(log)(apiHandler)
	apiHandler = middleware.LoggingMiddleware(log)(apiHandler)
	apiHandler = middleware.ContextTimeoutMiddleware(30 * time.Second)(apiHandler)

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.ServerPort),
		Handler:      apiHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Info(ctx, "HTTP server listening",
			"port", cfg.ServerPort,
			"address", server.Addr,
		)

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(ctx, "HTTP server error", err,
				"port", cfg.ServerPort,
			)
		}
	}()

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan

	log.Info(ctx, "Shutting down server...")

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error(shutdownCtx, "Server shutdown error", err)
	}

	log.Info(ctx, "Server stopped")
}
