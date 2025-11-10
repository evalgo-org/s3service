package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"eve.evalgo.org/semantic"
	"eve.evalgo.org/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/labstack/echo/v4"
)

// handleSemanticAction handles Schema.org JSON-LD actions for S3 operations
func handleSemanticAction(c echo.Context) error {
	// Read request body
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Failed to read request body: %v", err))
	}

	// Parse as SemanticAction
	action, err := semantic.ParseSemanticAction(body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Failed to parse action: %v", err))
	}

	// Dispatch to registered handler using the ActionRegistry
	// No switch statement needed - handlers are registered at startup
	return semantic.Handle(c, action)
}

// executeUploadAction handles file upload to S3 operations
func executeUploadActionImpl(c echo.Context, action *semantic.SemanticAction) error {
	ctx := context.Background()

	// Extract S3 bucket and object using helpers
	bucket, err := semantic.GetS3BucketFromAction(action)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to extract S3 bucket", err)
	}

	object, err := semantic.GetS3ObjectFromAction(action)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to extract S3 object", err)
	}

	// Extract S3 credentials
	url, region, accessKey, secretKey, bucketName, err := semantic.ExtractS3Credentials(bucket)
	_ = region // May be used for multi-region support
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to extract S3 credentials", err)
	}

	// Get file path from object
	filePath := object.ContentUrl
	if filePath == "" {
		return semantic.ReturnActionError(c, action, "Object contentUrl (file path) is required", nil)
	}

	// Determine S3 key
	s3Key := semantic.GetS3TargetUrlFromAction(action)
	if s3Key == "" {
		s3Key = object.Identifier
	}
	if s3Key == "" {
		s3Key = filepath.Base(filePath)
	}

	// Use EVE's HetznerUploadFile function
	if err := storage.HetznerUploadFile(ctx, url, accessKey, secretKey, bucketName, filePath, s3Key); err != nil {
		return semantic.ReturnActionError(c, action, "Failed to upload file", err)
	}

	// Get file info for result
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		// Use semantic Result structure
		action.Result = &semantic.SemanticResult{
			Type:   "DigitalDocument",
			Format: object.EncodingFormat,
			Value: map[string]interface{}{
				"contentUrl":     fmt.Sprintf("s3://%s/%s", bucketName, s3Key),
				"name":           filepath.Base(filePath),
				"contentSize":    fileInfo.Size(),
				"encodingFormat": object.EncodingFormat,
				"uploadDate":     time.Now().Format(time.RFC3339),
			},
		}
	}

	semantic.SetSuccessOnAction(action)
	return c.JSON(http.StatusOK, action)
}

// executeDownloadAction handles file download from S3 operations
func executeDownloadActionImpl(c echo.Context, action *semantic.SemanticAction) error {
	ctx := context.Background()

	// Extract S3 bucket and object using helpers
	bucket, err := semantic.GetS3BucketFromAction(action)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to extract S3 bucket", err)
	}

	object, err := semantic.GetS3ObjectFromAction(action)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to extract S3 object", err)
	}

	// Extract S3 credentials
	url, region, accessKey, secretKey, bucketName, err := semantic.ExtractS3Credentials(bucket)
	_ = region // May be used for multi-region support
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to extract S3 credentials", err)
	}

	// Get S3 key from object
	s3Key := object.Identifier
	if s3Key == "" {
		s3Key = object.Name
	}
	if s3Key == "" {
		return semantic.ReturnActionError(c, action, "Object identifier (S3 key) is required", nil)
	}

	// Determine local download path
	downloadPath := object.ContentUrl
	if downloadPath == "" {
		downloadPath = filepath.Join("/tmp", filepath.Base(s3Key))
	}

	// Create S3 client
	client, err := createS3Client(ctx, url, region, accessKey, secretKey)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to create S3 client", err)
	}

	// Download file
	result, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to download file", err)
	}
	defer func() { _ = result.Body.Close() }()

	// Write to local file
	outFile, err := os.Create(downloadPath)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to create local file", err)
	}
	defer func() { _ = outFile.Close() }()

	size, err := io.Copy(outFile, result.Body)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to write file", err)
	}

	// Use semantic Result structure
	action.Result = &semantic.SemanticResult{
		Type:   "DigitalDocument",
		Format: object.EncodingFormat,
		Value: map[string]interface{}{
			"contentUrl":     downloadPath,
			"name":           filepath.Base(s3Key),
			"contentSize":    size,
			"encodingFormat": object.EncodingFormat,
		},
	}

	semantic.SetSuccessOnAction(action)
	return c.JSON(http.StatusOK, action)
}

// executeDeleteAction handles file deletion from S3 operations
func executeDeleteActionImpl(c echo.Context, action *semantic.SemanticAction) error {
	ctx := context.Background()

	// Extract S3 bucket and object using helpers
	bucket, err := semantic.GetS3BucketFromAction(action)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to extract S3 bucket", err)
	}

	object, err := semantic.GetS3ObjectFromAction(action)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to extract S3 object", err)
	}

	// Extract S3 credentials
	url, region, accessKey, secretKey, bucketName, err := semantic.ExtractS3Credentials(bucket)
	_ = region // May be used for multi-region support
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to extract S3 credentials", err)
	}

	// Get S3 key from object
	s3Key := object.Identifier
	if s3Key == "" {
		s3Key = object.Name
	}
	if s3Key == "" {
		return semantic.ReturnActionError(c, action, "Object identifier (S3 key) is required", nil)
	}

	// Create S3 client
	client, err := createS3Client(ctx, url, region, accessKey, secretKey)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to create S3 client", err)
	}

	// Delete object
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to delete file", err)
	}

	semantic.SetSuccessOnAction(action)
	return c.JSON(http.StatusOK, action)
}

// executeListAction handles listing objects in S3 bucket
func executeListActionImpl(c echo.Context, action *semantic.SemanticAction) error {
	ctx := context.Background()

	// Extract S3 bucket using helper
	bucket, err := semantic.GetS3BucketFromAction(action)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to extract S3 bucket", err)
	}

	// Extract S3 credentials
	url, region, accessKey, secretKey, bucketName, err := semantic.ExtractS3Credentials(bucket)
	_ = region // May be used for multi-region support
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to extract S3 credentials", err)
	}

	// Create S3 client
	client, err := createS3Client(ctx, url, region, accessKey, secretKey)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to create S3 client", err)
	}

	// List objects with optional prefix from query
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	}
	if query, ok := action.Properties["query"].(string); ok && query != "" {
		input.Prefix = aws.String(query)
	}

	result, err := client.ListObjectsV2(ctx, input)
	if err != nil {
		return semantic.ReturnActionError(c, action, "Failed to list objects", err)
	}

	// Build result list
	objects := make([]interface{}, 0, len(result.Contents))
	for _, obj := range result.Contents {
		objects = append(objects, map[string]interface{}{
			"contentUrl":     fmt.Sprintf("s3://%s/%s", bucketName, *obj.Key),
			"name":           filepath.Base(*obj.Key),
			"contentSize":    *obj.Size,
			"encodingFormat": "application/octet-stream",
			"uploadDate":     obj.LastModified.Format(time.RFC3339),
		})
	}

	// Use semantic Result structure for list results
	action.Result = &semantic.SemanticResult{
		Type:   "Dataset",
		Format: "application/json",
		Value:  objects,
	}

	semantic.SetSuccessOnAction(action)
	return c.JSON(http.StatusOK, action)
}

// ============================================================================
// Helper Functions
// ============================================================================

// createS3Client creates an AWS S3 client configured for Hetzner or other S3-compatible storage
func createS3Client(ctx context.Context, endpoint, region, accessKey, secretKey string) (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	}), nil
}

// executeUploadAction wraps the implementation to match ActionHandler signature
func executeUploadAction(c echo.Context, actionInterface interface{}) error {
	action, ok := actionInterface.(*semantic.SemanticAction)
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid action type")
	}
	return executeUploadActionImpl(c, action)
}

// executeDownloadAction wraps the implementation to match ActionHandler signature
func executeDownloadAction(c echo.Context, actionInterface interface{}) error {
	action, ok := actionInterface.(*semantic.SemanticAction)
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid action type")
	}
	return executeDownloadActionImpl(c, action)
}

// executeDeleteAction wraps the implementation to match ActionHandler signature
func executeDeleteAction(c echo.Context, actionInterface interface{}) error {
	action, ok := actionInterface.(*semantic.SemanticAction)
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid action type")
	}
	return executeDeleteActionImpl(c, action)
}

// executeListAction wraps the implementation to match ActionHandler signature
func executeListAction(c echo.Context, actionInterface interface{}) error {
	action, ok := actionInterface.(*semantic.SemanticAction)
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid action type")
	}
	return executeListActionImpl(c, action)
}
