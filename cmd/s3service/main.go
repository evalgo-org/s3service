package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"eve.evalgo.org/common"
	evehttp "eve.evalgo.org/http"
	"eve.evalgo.org/registry"
	"eve.evalgo.org/statemanager"
	"eve.evalgo.org/tracing"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// Initialize logger
	logger := common.ServiceLogger("s3service", "1.0.0")

	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// Initialize tracing (gracefully disabled if unavailable)
	if tracer := tracing.Init(tracing.InitConfig{
		ServiceID:        "s3service",
		DisableIfMissing: true,
	}); tracer != nil {
		e.Use(tracer.Middleware())
	}

	// EVE health check
	e.GET("/health", evehttp.HealthCheckHandler("s3service", "1.0.0"))

	// Documentation endpoint
	e.GET("/v1/api/docs", evehttp.DocumentationHandler(evehttp.ServiceDocConfig{
		ServiceID:    "s3service",
		ServiceName:  "S3 Object Storage Service",
		Description:  "S3-compatible object storage with support for AWS S3, Hetzner, and others",
		Version:      "v1",
		Port:         8092,
		Capabilities: []string{"object-storage", "s3", "semantic-actions", "state-tracking"},
		Endpoints: []evehttp.EndpointDoc{
			{
				Method:      "POST",
				Path:        "/v1/api/semantic/action",
				Description: "Execute S3 operations via semantic actions (primary interface)",
			},
			{
				Method:      "POST",
				Path:        "/v1/api/objects",
				Description: "Upload object (REST convenience - converts to CreateAction)",
			},
			{
				Method:      "GET",
				Path:        "/v1/api/objects/:key",
				Description: "Download object (REST convenience - converts to SearchAction)",
			},
			{
				Method:      "DELETE",
				Path:        "/v1/api/objects/:key",
				Description: "Delete object (REST convenience - converts to DeleteAction)",
			},
			{
				Method:      "GET",
				Path:        "/v1/api/buckets",
				Description: "List buckets (REST convenience - converts to SearchAction)",
			},
			{
				Method:      "POST",
				Path:        "/v1/api/buckets",
				Description: "Create bucket (REST convenience - converts to CreateAction)",
			},
			{
				Method:      "GET",
				Path:        "/health",
				Description: "Health check endpoint",
			},
		},
	}))

	// Initialize state manager
	sm := statemanager.New(statemanager.Config{
		ServiceName:   "s3service",
		MaxOperations: 100,
	})

	// Register state endpoints
	apiGroup := e.Group("/v1/api")
	sm.RegisterRoutes(apiGroup)

	// API Key middleware
	apiKey := os.Getenv("S3_API_KEY")
	apiKeyMiddleware := evehttp.APIKeyMiddleware(apiKey)

	// Semantic action endpoint (primary interface)
	apiGroup.POST("/semantic/action", handleSemanticAction, apiKeyMiddleware)

	// REST endpoints (convenience adapters that convert to semantic actions)
	registerRESTEndpoints(apiGroup, apiKeyMiddleware)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8092"
	}

	// Auto-register with registry service if REGISTRYSERVICE_API_URL is set
	portInt, _ := strconv.Atoi(port)
	if _, err := registry.AutoRegister(registry.AutoRegisterConfig{
		ServiceID:    "s3service",
		ServiceName:  "S3 Object Storage Service",
		Description:  "S3-compatible object storage with support for AWS S3, Hetzner, and others",
		Port:         portInt,
		Directory:    "/home/opunix/s3service",
		Binary:       "s3service",
		Version:      "v1",
		Capabilities: []string{"object-storage", "s3", "semantic-actions", "state-tracking"},
		APIVersions: []registry.APIVersion{
			{
				Version:       "v1",
				URL:           fmt.Sprintf("http://localhost:%d/v1", portInt),
				Documentation: fmt.Sprintf("http://localhost:%d/v1/api/docs", portInt),
				IsDefault:     true,
				Status:        "stable",
				ReleaseDate:   "2024-01-01",
				Capabilities:  []string{"object-storage", "s3", "semantic-actions"},
			},
		},
	}); err != nil {
		logger.WithError(err).Error("Failed to register with registry")
	}

	// Start server in goroutine
	go func() {
		logger.Infof("Starting S3 Semantic Service on port %s", port)
		logger.Info("Supports Hetzner S3, AWS S3, and S3-compatible storage")
		if err := e.Start(":" + port); err != nil {
			logger.WithError(err).Error("Server error")
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Unregister from registry
	if err := registry.AutoUnregister("s3service"); err != nil {
		logger.WithError(err).Error("Failed to unregister from registry")
	}

	// Shutdown server
	if err := e.Close(); err != nil {
		logger.WithError(err).Error("Error during shutdown")
	}

	logger.Info("Server stopped")
}
