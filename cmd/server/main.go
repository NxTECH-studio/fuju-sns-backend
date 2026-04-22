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
	"github.com/fuju/backend/internal/tagextractor"
	adminusecase "github.com/fuju/backend/internal/usecase/admin"
	badgeusecase "github.com/fuju/backend/internal/usecase/badge"
	followusecase "github.com/fuju/backend/internal/usecase/follow"
	imageusecase "github.com/fuju/backend/internal/usecase/image"
	ogpusecase "github.com/fuju/backend/internal/usecase/ogp"
	postusecase "github.com/fuju/backend/internal/usecase/post"
	timelineusecase "github.com/fuju/backend/internal/usecase/timeline"
	userusecase "github.com/fuju/backend/internal/usecase/user"
	"github.com/fuju/backend/pkg/authcore"
	"github.com/fuju/backend/pkg/logger"
	"github.com/fuju/backend/pkg/ogp"
	"github.com/fuju/backend/pkg/storage"
)

// Version is the release identifier, overridden at build time via
// `-ldflags "-X main.Version=..."`. Defaults to "dev" for local builds.
// BuildTime is the UTC timestamp of the binary's build, also injected
// via ldflags. "unknown" until set by the release pipeline.
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

	// Repositories (in-memory for now). The LinkStore backs the
	// post_images / post_tags cross-repo relations.
	links := inmemory.NewLinkStore()
	userRepo := inmemory.NewUserRepository()
	postRepo := inmemory.NewPostRepository(links)
	imageRepo := inmemory.NewImageRepository(links)
	tagRepo := inmemory.NewTagRepository(links)
	likeRepo := inmemory.NewLikeRepository()
	badgeRepo := inmemory.NewBadgeRepository()
	followRepo := inmemory.NewFollowRepository()
	ogpCacheRepo := inmemory.NewOGPCacheRepository(links)
	ogpJobQueue := inmemory.NewOGPJobQueue()

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

	// TagExtractor (MVP: regex + empty keyword dictionary). Keyword list
	// ships empty for now; load from config when a dictionary source lands.
	tagEx := tagextractor.NewRegexTagExtractor(nil)

	postHydrator := postusecase.NewHydrator(imageRepo, tagRepo, likeRepo, userRepo, followRepo, ogpCacheRepo)

	ogpEnqueuer := ogpusecase.NewEnqueuer(ogpCacheRepo, ogpJobQueue, postRepo, log)

	postGetUC := postusecase.NewGetPostUseCase(postRepo, postHydrator)
	postCreateUC := postusecase.
		NewCreatePostUseCase(postRepo, imageRepo, tagRepo, tagEx).
		WithCommitHook(ogpEnqueuer.EnqueueForPost)
	postDeleteUC := postusecase.NewDeletePostUseCase(postRepo)
	postListUC := postusecase.NewListPostsUseCase(postRepo, postHydrator)
	postRepliesUC := postusecase.NewListRepliesUseCase(postRepo, postHydrator)
	postLikeUC := postusecase.NewLikePostUseCase(postRepo, likeRepo)
	postUnlikeUC := postusecase.NewUnlikePostUseCase(postRepo, likeRepo)

	followUC := followusecase.NewUseCase(userRepo, followRepo)
	unfollowUC := followusecase.NewUnfollowUseCase(userRepo, followRepo)
	listFollowersUC := followusecase.NewListFollowersUseCase(userRepo, followRepo)
	listFollowingUC := followusecase.NewListFollowingUseCase(userRepo, followRepo)

	homeTimelineUC := timelineusecase.NewHomeTimelineUseCase(postRepo, followRepo, postHydrator)
	userTimelineUC := timelineusecase.NewUserTimelineUseCase(postRepo, postHydrator)
	globalTimelineUC := timelineusecase.NewGlobalTimelineUseCase(postRepo, postHydrator)

	adminChecker := adminusecase.NewChecker(userRepo)
	badgeGetUC := badgeusecase.NewGetUserBadgesUseCase(badgeRepo)
	badgeBatchUC := badgeusecase.NewListUserBadgesBatchUseCase(badgeRepo)
	badgeListUC := badgeusecase.NewListBadgesUseCase(badgeRepo)
	badgeCreateUC := badgeusecase.NewCreateBadgeUseCase(badgeRepo, adminChecker)
	badgeUpdateUC := badgeusecase.NewUpdateBadgeUseCase(badgeRepo, adminChecker)
	badgeGrantUC := badgeusecase.NewGrantBadgeUseCase(badgeRepo, userRepo, adminChecker)
	badgeRevokeUC := badgeusecase.NewRevokeBadgeUseCase(badgeRepo, adminChecker)

	// Handlers
	healthHandler := handler.NewHealthHandler()
	userHandler := handler.NewUserHandler(userGetUC, userUpdateUC, userListUC, userHydrateUC, badgeGetUC, badgeBatchUC)
	postHandler := handler.NewPostHandler(postGetUC, postCreateUC, postDeleteUC, postListUC, postRepliesUC, postLikeUC, postUnlikeUC)
	badgeHandler := handler.NewBadgeHandler(badgeListUC, badgeCreateUC, badgeUpdateUC, badgeGrantUC, badgeRevokeUC)
	followHandler := handler.NewFollowHandler(followUC, unfollowUC, listFollowersUC, listFollowingUC)
	timelineHandler := handler.NewTimelineHandler(homeTimelineUC, userTimelineUC, globalTimelineUC)

	// Optional R2 / image handlers.
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

	// Session cookie policy for the Bearer→Cookie handoff endpoints.
	// SameSite is parsed once by config.Load (Validate caches the
	// http.SameSite value), so wiring here is a plain struct copy.
	sessionCookieCfg := handler.SessionCookieConfig{
		Name:           cfg.SessionCookieName,
		Secure:         cfg.SessionCookieSecure,
		SameSite:       cfg.SessionCookieSameSiteMode(),
		Domain:         cfg.SessionCookieDomain,
		Path:           "/",
		FallbackMaxAge: cfg.SessionCookieFallbackMaxAge,
	}
	sessionHandler := handler.NewSessionHandler(sessionCookieCfg)

	// Middleware chain for authenticated routes: AuthMiddleware (introspect
	// Bearer or Cookie token + set sub / access token / expiry on ctx)
	// followed by HydrateUserMiddleware (load / upsert user row).
	authMW := middleware.AuthMiddleware(cachedAuthCore, middleware.AuthMiddlewareConfig{
		CookieName: cfg.SessionCookieName,
	})
	hydrateMW := middleware.HydrateUserMiddleware(userHydrateUC)
	adminMW := middleware.AdminMiddleware()
	authed := func(next http.Handler) http.Handler {
		return authMW(hydrateMW(next))
	}
	adminOnly := func(next http.Handler) http.Handler {
		return authMW(hydrateMW(adminMW(next)))
	}

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /health", healthHandler.Health)

	// Session handoff (browser auth cookie). POST requires a valid
	// Bearer token; AuthMiddleware introspects it and the handler
	// flips the value into an HttpOnly cookie. DELETE is intentionally
	// public and idempotent so a stale tab can clear its cookie even
	// after the access token has expired.
	mux.Handle("POST /v1/auth/session", authMW(http.HandlerFunc(sessionHandler.Issue)))
	mux.HandleFunc("DELETE /v1/auth/session", sessionHandler.Revoke)

	// Me: authenticated caller's own record (lazy-created / hydrated).
	mux.Handle("GET /me", authed(http.HandlerFunc(userHandler.Me)))

	// Users
	mux.HandleFunc("GET /users", userHandler.ListUsers)
	mux.HandleFunc("GET /users/{sub}", userHandler.GetUser)
	mux.Handle("PUT /users/{sub}", authed(http.HandlerFunc(userHandler.UpdateUser)))

	// Posts. Authenticated routes run through the auth chain so the
	// handler can read viewerSub via auth.GetSubFromContext for public
	// reads; unauthenticated GETs simply see no sub on the context.
	mux.HandleFunc("GET /posts", postHandler.ListPosts)
	mux.Handle("POST /posts", authed(http.HandlerFunc(postHandler.CreatePost)))
	mux.HandleFunc("GET /posts/{id}", postHandler.GetPost)
	mux.HandleFunc("GET /posts/{id}/replies", postHandler.ListReplies)
	mux.Handle("DELETE /posts/{id}", authed(http.HandlerFunc(postHandler.DeletePost)))
	mux.Handle("POST /posts/{id}/like", authed(http.HandlerFunc(postHandler.LikePost)))
	mux.Handle("DELETE /posts/{id}/like", authed(http.HandlerFunc(postHandler.UnlikePost)))

	// Images (only if R2 configured)
	if imageHandler != nil {
		mux.Handle("POST /v1/images", authed(http.HandlerFunc(imageHandler.UploadImage)))
		mux.Handle("GET /v1/images", authed(http.HandlerFunc(imageHandler.GetUserImages)))
		mux.Handle("DELETE /v1/images/{id}", authed(http.HandlerFunc(imageHandler.DeleteImage)))
	}

	// Follow / followers / following. Public reads, authenticated writes.
	mux.Handle("POST /users/{sub}/follow", authed(http.HandlerFunc(followHandler.Follow)))
	mux.Handle("DELETE /users/{sub}/follow", authed(http.HandlerFunc(followHandler.Unfollow)))
	mux.HandleFunc("GET /users/{sub}/followers", followHandler.ListFollowers)
	mux.HandleFunc("GET /users/{sub}/following", followHandler.ListFollowing)

	// Timelines. Home requires auth; user/global are public (viewer
	// context is optional and, when present, populates liked_by_viewer
	// and following_author on each post).
	mux.Handle("GET /timeline/home", authed(http.HandlerFunc(timelineHandler.Home)))
	mux.HandleFunc("GET /timeline/user/{sub}", timelineHandler.User)
	mux.HandleFunc("GET /timeline/global", timelineHandler.Global)

	// Admin badges
	mux.Handle("GET /v1/admin/badges", adminOnly(http.HandlerFunc(badgeHandler.ListBadges)))
	mux.Handle("POST /v1/admin/badges", adminOnly(http.HandlerFunc(badgeHandler.CreateBadge)))
	mux.Handle("PUT /v1/admin/badges/{id}", adminOnly(http.HandlerFunc(badgeHandler.UpdateBadge)))
	mux.Handle("POST /v1/admin/users/{sub}/badges", adminOnly(http.HandlerFunc(badgeHandler.GrantBadge)))
	mux.Handle("DELETE /v1/admin/users/{sub}/badges/{badge_id}", adminOnly(http.HandlerFunc(badgeHandler.RevokeBadge)))

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

	// OGP background worker. Tied to the background context that is
	// cancelled at shutdown so the goroutine exits cleanly.
	ogpFetcher := ogp.NewFetcher(&ogp.Options{UserAgent: cfg.OGPUserAgent})
	ogpWorker := ogpusecase.NewWorker(ogpJobQueue, ogpCacheRepo, postRepo, ogpFetcher, log)
	go func() {
		log.Info(ctx, "OGP worker started")
		ogpWorker.Run(ctx)
		log.Info(ctx, "OGP worker stopped")
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

	// Cancel the background context so the OGP worker exits.
	cancelBackground()

	log.Info(ctx, "Server stopped")
}
