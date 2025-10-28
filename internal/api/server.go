package api

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/agentregistry-dev/agentregistry/internal/database"
	"github.com/gin-gonic/gin"
)

//go:embed ui/dist/*
var embeddedUI embed.FS

// StartServer starts the API server with embedded UI
func StartServer(port string) error {
	// Initialize database
	if err := database.Initialize(); err != nil {
		return err
	}
	defer func() {
		_ = database.Close()
	}()

	router := gin.Default()

	// API routes
	api := router.Group("/api")
	{
		api.GET("/registries", getRegistries)
		api.GET("/servers", getServers)
		api.GET("/skills", getSkills)
		api.GET("/installations", getInstallations)
		api.GET("/health", healthCheck)
	}

	// Serve embedded UI
	// Try to serve from embedded filesystem
	uiFS, err := fs.Sub(embeddedUI, "ui/dist")
	if err != nil {
		// If embedded UI doesn't exist yet (during development), serve a simple message
		router.NoRoute(func(c *gin.Context) {
			c.String(http.StatusOK, "UI not built yet. Run 'make build-ui' to build the Next.js app.")
		})
	} else {
		// Serve static files using http.FileServer for proper Next.js static export handling
		fileServer := http.FileServer(http.FS(uiFS))
		router.NoRoute(func(c *gin.Context) {
			// Let the file server handle the request directly
			// This properly handles index.html, trailing slashes, and static assets
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
	}

	return router.Run(":" + port)
}

// API handlers

func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "arctl API is running",
	})
}

func getRegistries(c *gin.Context) {
	registries, err := database.GetRegistries()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, registries)
}

func getServers(c *gin.Context) {
	servers, err := database.GetServers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, servers)
}

func getSkills(c *gin.Context) {
	skills, err := database.GetSkills()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, skills)
}

func getInstallations(c *gin.Context) {
	installations, err := database.GetInstallations()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, installations)
}
