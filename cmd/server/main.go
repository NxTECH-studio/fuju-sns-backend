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
	"github.com/fuju/backend/pkg/authcore"
	"github.com/fuju/backend/pkg/logger"
	"github.com/fuju/backend/pkg/storage"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel)
	ctx, cancelBackground := context.WithCancel(context.Background())
	defer cancelBackground()

	log.Info(ctx, "Starting FUJU Backend Server",
		"version", Version,
		"build_time", BuildTime,
		"environment", cfg.Environment,
	)

	// Repositories (in-memory for now).
	userRepo := inmemory.NewUserRepository()
	postRepo := inmemory.NewPostRepository()
	commentRepo := inmemory.NewCommentRepository()

	// AuthCore client + short-lived introspection cache.
	authcoreClient := authcore.New(authcore.Options{
		BaseURL:        cfg.AuthCoreBaseURL,
		ClientID:       cfg.AuthCoreClientID,
		ClientSecret:   cfg.AuthCoreClientSecret,
		IntrospectPath: cfg.AuthCoreIntrospectPath,
		ProfilePath:    cfg.AuthCoreProfilePath,
	})
	cachedAuthCore := authcore.NewIntrospectCache(authcoreClient, cfg.AuthCoreIntrospectCacheTTL)
	cachedAuthCore.StartJanitor(ctx, cfg.AuthCoreIntrospectCacheTTL)

	// Use cases
	userGetUC := userusecase.NewGetUserUseCase(userRepo)
	userHydrateUC := userusecase.NewGetOrHydrateUserUseCase(userRepo, cachedAuthCore, cfg.AuthCoreProfileTTL, log)
	userUpdateUC := userusecase.NewUpdateUserProfileUseCase(userRepo)
	userListUC := userusecase.NewListUsersUseCase(userRepo)

	postGetUC := postusecase.NewGetPostUseCase(postRepo)
	postCreateUC := postusecase.NewCreatePostUseCase(postRepo)
	postDeleteUC := postusecase.NewDeletePostUseCase(postRepo)
	postListUC := postusecase.NewListPostsUseCase(postRepo)

	commentAddUC := commentusecase.NewAddCommentUseCase(commentRepo, postRepo)
	commentDeleteUC := commentusecase.NewDeleteCommentUseCase(commentRepo, postRepo)

	// Handlers
	healthHandler := handler.NewHealthHandler()
	userHandler := handler.NewUserHandler(userGetUC, userUpdateUC, userListUC, userHydrateUC)
	postHandler := handler.NewPostHandler(postGetUC, postCreateUC, postDeleteUC, postListUC)
	commentHandler := handler.NewCommentHandlerImpl(commentAddUC, commentDeleteUC)

	// Optional R2 / image handlers.
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

	// Middleware chain for authenticated routes: AuthMiddleware (introspect
	// Bearer token + set sub + set access token) followed by
	// HydrateUserMiddleware (load / upsert user row).
	authMW := middleware.AuthMiddleware(cachedAuthCore)
	hydrateMW := middleware.HydrateUserMiddleware(userHydrateUC)
	authed := func(next http.Handler) http.Handler {
		return authMW(hydrateMW(next))
	}

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /health", healthHandler.Health)

	// Me: authenticated caller's own record (lazy-created / hydrated).
	mux.Handle("GET /me", authed(http.HandlerFunc(userHandler.Me)))

	// Users
	mux.HandleFunc("GET /users", userHandler.ListUsers)
	mux.HandleFunc("GET /users/{sub}", userHandler.GetUser)
	mux.Handle("PUT /users/{sub}", authed(http.HandlerFunc(userHandler.UpdateUser)))

	// Posts
	mux.HandleFunc("GET /posts", postHandler.ListPosts)
	mux.Handle("POST /posts", authed(http.HandlerFunc(postHandler.CreatePost)))
	mux.HandleFunc("GET /posts/{id}", postHandler.GetPost)
	mux.Handle("DELETE /posts/{id}", authed(http.HandlerFunc(postHandler.DeletePost)))

	// Comments
	mux.Handle("POST /posts/{id}/comments", authed(http.HandlerFunc(commentHandler.AddComment)))
	mux.Handle("DELETE /posts/{post_id}/comments/{comment_id}", authed(http.HandlerFunc(commentHandler.DeleteComment)))

	// Images (only if R2 configured)
	if imageHandler != nil {
		mux.Handle("POST /v1/images", authed(http.HandlerFunc(imageHandler.UploadImage)))
		mux.Handle("GET /v1/images", authed(http.HandlerFunc(imageHandler.GetUserImages)))
		mux.Handle("DELETE /v1/images/{id}", authed(http.HandlerFunc(imageHandler.DeleteImage)))
	}

	var apiHandler http.Handler = mux
	apiHandler = middleware.CORSMiddleware(cfg.CORSAllowedOrigins)(apiHandler)
	apiHandler = middleware.RecoveryMiddleware(log)(apiHandler)
	apiHandler = middleware.LoggingMiddleware(log)(apiHandler)
	apiHandler = middleware.ContextTimeoutMiddleware(30 * time.Second)(apiHandler)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.ServerPort),
		Handler:      apiHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

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

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan

	log.Info(ctx, "Shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error(shutdownCtx, "Server shutdown error", err)
	}

	log.Info(ctx, "Server stopped")
}
