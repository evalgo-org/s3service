package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"eve.evalgo.org/semantic"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Test environment variables
var (
	testURL       string
	testAccessKey string
	testSecretKey string
	testBucket    string
	testServer    *httptest.Server
	testEcho      *echo.Echo
)

// setupTestServer creates a test HTTP server with the s3service handlers
func setupTestServer() {
	testEcho = echo.New()
	testEcho.Use(middleware.Logger())
	testEcho.Use(middleware.Recover())
	testEcho.POST("/v1/api/semantic/action", handleSemanticAction)
	testServer = httptest.NewServer(testEcho)
}

// teardownTestServer closes the test server
func teardownTestServer() {
	if testServer != nil {
		testServer.Close()
	}
}

// loadTestEnv loads test configuration from test.env file
func loadTestEnv(t *testing.T) {
	envFile := filepath.Join("..", "test.env")
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Skipf("Skipping integration test: test.env not found: %v", err)
		return
	}

	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		parts := bytes.SplitN(line, []byte("="), 2)
		if len(parts) != 2 {
			continue
		}
		key := string(bytes.TrimSpace(parts[0]))
		value := string(bytes.TrimSpace(parts[1]))

		switch key {
		case "URL":
			testURL = value
		case "ACCESS_KEY":
			testAccessKey = value
		case "SECRET_KEY":
			testSecretKey = value
		case "BUCKET":
			testBucket = value
		}
	}

	if testURL == "" || testAccessKey == "" || testSecretKey == "" || testBucket == "" {
		t.Skip("Skipping integration test: incomplete test.env configuration")
	}
}

// TestIntegration_UploadFile tests uploading a file to S3
func TestIntegration_UploadFile(t *testing.T) {
	loadTestEnv(t)
	setupTestServer()
	defer teardownTestServer()

	// Create a temporary test file
	testFile := "/tmp/s3service-test-upload.txt"
	testContent := []byte(fmt.Sprintf("S3 Service Integration Test - Upload - %s", time.Now().Format(time.RFC3339)))
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() { _ = os.Remove(testFile) }()

	// Create SemanticAction for upload
	action := semantic.NewSemanticS3UploadAction("test-upload-integration", "Integration Test Upload",
		&semantic.S3Object{
			Type:           "MediaObject",
			Identifier:     "test/integration-upload.txt",
			ContentUrl:     testFile,
			EncodingFormat: "text/plain",
		},
		semantic.NewS3Bucket(testBucket, testURL, "fsn1", testAccessKey, testSecretKey),
		"test/integration-upload.txt")

	// Marshal to JSON
	payload, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("Failed to marshal action: %v", err)
	}

	// Send request
	req := httptest.NewRequest(http.MethodPost, "/v1/api/semantic/action", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testEcho.ServeHTTP(rec, req)

	// Check response
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Parse response
	var result semantic.SemanticAction
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result.ActionStatus != "CompletedActionStatus" {
		t.Errorf("Expected CompletedActionStatus, got %s", result.ActionStatus)
	}

	if result.Properties["result"] == nil {
		t.Error("Expected result in properties, got nil")
	}

	t.Logf("✓ Upload test passed - uploaded to s3://%s/test/integration-upload.txt", testBucket)
}

// TestIntegration_ListObjects tests listing objects in S3
func TestIntegration_ListObjects(t *testing.T) {
	loadTestEnv(t)
	setupTestServer()
	defer teardownTestServer()

	// Create SemanticAction for list
	action := semantic.NewSemanticS3ListAction("test-list-integration", "Integration Test List", "test/",
		semantic.NewS3Bucket(testBucket, testURL, "fsn1", testAccessKey, testSecretKey))

	// Marshal to JSON
	payload, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("Failed to marshal action: %v", err)
	}

	// Send request
	req := httptest.NewRequest(http.MethodPost, "/v1/api/semantic/action", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testEcho.ServeHTTP(rec, req)

	// Check response
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Parse response
	var result semantic.SemanticAction
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result.ActionStatus != "CompletedActionStatus" {
		t.Errorf("Expected CompletedActionStatus, got %s", result.ActionStatus)
	}

	// Access result from properties
	resultData, ok := result.Properties["result"].([]interface{})
	if !ok {
		resultData = []interface{}{}
	}

	t.Logf("✓ List test passed - found %d objects with prefix 'test/'", len(resultData))
	for i, obj := range resultData {
		if objMap, ok := obj.(map[string]interface{}); ok {
			identifier := objMap["identifier"]
			contentSize := objMap["contentSize"]
			t.Logf("  [%d] %v (%v bytes)", i+1, identifier, contentSize)
		}
	}
}

// TestIntegration_DownloadFile tests downloading a file from S3
func TestIntegration_DownloadFile(t *testing.T) {
	loadTestEnv(t)
	setupTestServer()
	defer teardownTestServer()

	// Download path
	downloadPath := "/tmp/s3service-test-download.txt"
	defer func() { _ = os.Remove(downloadPath) }()

	// Create SemanticAction for download
	action := semantic.NewSemanticS3DownloadAction("test-download-integration", "Integration Test Download",
		&semantic.S3Object{
			Type:           "MediaObject",
			Identifier:     "test/integration-upload.txt",
			ContentUrl:     downloadPath,
			EncodingFormat: "text/plain",
		},
		semantic.NewS3Bucket(testBucket, testURL, "fsn1", testAccessKey, testSecretKey))

	// Marshal to JSON
	payload, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("Failed to marshal action: %v", err)
	}

	// Send request
	req := httptest.NewRequest(http.MethodPost, "/v1/api/semantic/action", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testEcho.ServeHTTP(rec, req)

	// Check response
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Parse response
	var result semantic.SemanticAction
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result.ActionStatus != "CompletedActionStatus" {
		t.Errorf("Expected CompletedActionStatus, got %s", result.ActionStatus)
	}

	// Verify file was downloaded
	if _, err := os.Stat(downloadPath); os.IsNotExist(err) {
		t.Error("Downloaded file does not exist")
	}

	// Read and verify content
	content, err := os.ReadFile(downloadPath)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	if len(content) == 0 {
		t.Error("Downloaded file is empty")
	}

	t.Logf("✓ Download test passed - downloaded %d bytes", len(content))
	t.Logf("  Content preview: %s", string(content[:min(100, len(content))]))
}

// TestIntegration_DeleteFile tests deleting a file from S3
func TestIntegration_DeleteFile(t *testing.T) {
	loadTestEnv(t)
	setupTestServer()
	defer teardownTestServer()

	// Create SemanticAction for delete
	action := semantic.NewSemanticS3DeleteAction("test-delete-integration", "Integration Test Delete",
		&semantic.S3Object{
			Type:       "MediaObject",
			Identifier: "test/integration-upload.txt",
		},
		semantic.NewS3Bucket(testBucket, testURL, "fsn1", testAccessKey, testSecretKey))

	// Marshal to JSON
	payload, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("Failed to marshal action: %v", err)
	}

	// Send request
	req := httptest.NewRequest(http.MethodPost, "/v1/api/semantic/action", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testEcho.ServeHTTP(rec, req)

	// Check response
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Parse response
	var result semantic.SemanticAction
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result.ActionStatus != "CompletedActionStatus" {
		t.Errorf("Expected CompletedActionStatus, got %s", result.ActionStatus)
	}

	t.Logf("✓ Delete test passed - deleted test/integration-upload.txt")
}

// TestIntegration_FullWorkflow tests complete upload → list → download → delete workflow
func TestIntegration_FullWorkflow(t *testing.T) {
	loadTestEnv(t)
	setupTestServer()
	defer teardownTestServer()

	ctx := context.Background()
	_ = ctx

	testKey := fmt.Sprintf("test/workflow-%d.txt", time.Now().Unix())
	testContent := []byte("Full workflow integration test")
	testFile := "/tmp/s3-workflow-upload.txt"
	downloadFile := "/tmp/s3-workflow-download.txt"

	defer func() { _ = os.Remove(testFile) }()
	defer func() { _ = os.Remove(downloadFile) }()

	// Step 1: Create test file
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Step 2: Upload
	t.Log("Step 1: Uploading file...")
	uploadAction := semantic.NewSemanticS3UploadAction("test-workflow-upload", "Workflow Upload",
		&semantic.S3Object{
			Type:           "MediaObject",
			Identifier:     testKey,
			ContentUrl:     testFile,
			EncodingFormat: "text/plain",
		},
		semantic.NewS3Bucket(testBucket, testURL, "fsn1", testAccessKey, testSecretKey),
		testKey)

	if err := executeAction(t, testEcho, uploadAction); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	t.Log("  ✓ Upload successful")

	// Step 3: List
	t.Log("Step 2: Listing objects...")
	listAction := semantic.NewSemanticS3ListAction("test-workflow-list", "Workflow List", "test/workflow-",
		semantic.NewS3Bucket(testBucket, testURL, "fsn1", testAccessKey, testSecretKey))

	if err := executeAction(t, testEcho, listAction); err != nil {
		t.Fatalf("List failed: %v", err)
	}
	t.Log("  ✓ List successful")

	// Step 4: Download
	t.Log("Step 3: Downloading file...")
	downloadAction := semantic.NewSemanticS3DownloadAction("test-workflow-download", "Workflow Download",
		&semantic.S3Object{
			Type:       "MediaObject",
			Identifier: testKey,
			ContentUrl: downloadFile,
		},
		semantic.NewS3Bucket(testBucket, testURL, "fsn1", testAccessKey, testSecretKey))

	if err := executeAction(t, testEcho, downloadAction); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	t.Log("  ✓ Download successful")

	// Verify downloaded content
	downloaded, err := os.ReadFile(downloadFile)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	if !bytes.Equal(downloaded, testContent) {
		t.Errorf("Downloaded content mismatch:\nExpected: %s\nGot: %s", testContent, downloaded)
	}

	// Step 5: Delete
	t.Log("Step 4: Deleting file...")
	deleteAction := semantic.NewSemanticS3DeleteAction("test-workflow-delete", "Workflow Delete",
		&semantic.S3Object{Type: "MediaObject", Identifier: testKey},
		semantic.NewS3Bucket(testBucket, testURL, "fsn1", testAccessKey, testSecretKey))

	if err := executeAction(t, testEcho, deleteAction); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	t.Log("  ✓ Delete successful")

	t.Log("✓ Full workflow test passed: upload → list → download → delete")
}

// Helper function to execute an action
func executeAction(t *testing.T, e *echo.Echo, action interface{}) error {
	payload, err := json.Marshal(action)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/api/semantic/action", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		body, _ := io.ReadAll(rec.Body)
		return fmt.Errorf("HTTP %d: %s", rec.Code, body)
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
