package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"
)

// REST endpoint request types

type UploadObjectRequest struct {
	Key     string `json:"key"`
	Content string `json:"content"` // base64 encoded
	Bucket  string `json:"bucket,omitempty"`
}

type CreateBucketRequest struct {
	Name string `json:"name"`
}

// registerRESTEndpoints adds REST endpoints that convert to semantic actions
func registerRESTEndpoints(apiGroup *echo.Group, apiKeyMiddleware echo.MiddlewareFunc) {
	// POST /v1/api/objects - Upload object
	apiGroup.POST("/objects", uploadObjectREST, apiKeyMiddleware)

	// GET /v1/api/objects/:key - Download object
	apiGroup.GET("/objects/:key", getObjectREST, apiKeyMiddleware)

	// DELETE /v1/api/objects/:key - Delete object
	apiGroup.DELETE("/objects/:key", deleteObjectREST, apiKeyMiddleware)

	// GET /v1/api/buckets - List buckets
	apiGroup.GET("/buckets", listBucketsREST, apiKeyMiddleware)

	// POST /v1/api/buckets - Create bucket
	apiGroup.POST("/buckets", createBucketREST, apiKeyMiddleware)
}

// uploadObjectREST handles REST POST /v1/api/objects
func uploadObjectREST(c echo.Context) error {
	var req UploadObjectRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Invalid request: %v", err)})
	}

	if req.Key == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "key is required"})
	}
	if req.Content == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "content is required"})
	}

	// Convert to JSON-LD CreateAction
	action := map[string]interface{}{
		"@context": "https://schema.org",
		"@type":    "CreateAction",
		"object": map[string]interface{}{
			"@type":      "DigitalDocument",
			"identifier": req.Key,
			"text":       req.Content,
		},
	}

	if req.Bucket != "" {
		action["instrument"] = map[string]interface{}{
			"@type": "PropertyValue",
			"name":  "bucket",
			"value": req.Bucket,
		}
	}

	return callSemanticHandler(c, action)
}

// getObjectREST handles REST GET /v1/api/objects/:key
func getObjectREST(c echo.Context) error {
	key := c.Param("key")
	if key == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "key is required"})
	}

	bucket := c.QueryParam("bucket")

	// Convert to JSON-LD SearchAction
	action := map[string]interface{}{
		"@context": "https://schema.org",
		"@type":    "SearchAction",
		"object": map[string]interface{}{
			"@type":      "DigitalDocument",
			"identifier": key,
		},
	}

	if bucket != "" {
		action["instrument"] = map[string]interface{}{
			"@type": "PropertyValue",
			"name":  "bucket",
			"value": bucket,
		}
	}

	return callSemanticHandler(c, action)
}

// deleteObjectREST handles REST DELETE /v1/api/objects/:key
func deleteObjectREST(c echo.Context) error {
	key := c.Param("key")
	if key == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "key is required"})
	}

	bucket := c.QueryParam("bucket")

	// Convert to JSON-LD DeleteAction
	action := map[string]interface{}{
		"@context": "https://schema.org",
		"@type":    "DeleteAction",
		"object": map[string]interface{}{
			"@type":      "DigitalDocument",
			"identifier": key,
		},
	}

	if bucket != "" {
		action["instrument"] = map[string]interface{}{
			"@type": "PropertyValue",
			"name":  "bucket",
			"value": bucket,
		}
	}

	return callSemanticHandler(c, action)
}

// listBucketsREST handles REST GET /v1/api/buckets
func listBucketsREST(c echo.Context) error {
	// Convert to JSON-LD SearchAction
	action := map[string]interface{}{
		"@context": "https://schema.org",
		"@type":    "SearchAction",
		"query":    "list-buckets",
	}

	return callSemanticHandler(c, action)
}

// createBucketREST handles REST POST /v1/api/buckets
func createBucketREST(c echo.Context) error {
	var req CreateBucketRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Invalid request: %v", err)})
	}

	if req.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}

	// Convert to JSON-LD CreateAction
	action := map[string]interface{}{
		"@context": "https://schema.org",
		"@type":    "CreateAction",
		"object": map[string]interface{}{
			"@type":      "Thing",
			"name":       req.Name,
			"identifier": "bucket",
		},
	}

	return callSemanticHandler(c, action)
}

// callSemanticHandler converts action to JSON and calls the semantic action handler
func callSemanticHandler(c echo.Context, action map[string]interface{}) error {
	// Marshal action to JSON
	actionJSON, err := json.Marshal(action)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to marshal action: %v", err)})
	}

	// Create new request with JSON-LD body
	newReq := c.Request().Clone(c.Request().Context())
	newReq.Body = io.NopCloser(bytes.NewReader(actionJSON))
	newReq.Header.Set("Content-Type", "application/json")

	// Create new context with modified request
	newCtx := c.Echo().NewContext(newReq, c.Response())
	newCtx.SetPath(c.Path())
	newCtx.SetParamNames(c.ParamNames()...)
	newCtx.SetParamValues(c.ParamValues()...)

	// Call the existing semantic action handler
	return handleSemanticAction(newCtx)
}
