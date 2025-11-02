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

	// Use EVE library's ParseS3Action for routing and parsing
	action, err := semantic.ParseS3Action(body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Failed to parse action: %v", err))
	}

	// Route to appropriate handler based on type
	switch v := action.(type) {
	case *semantic.S3UploadAction:
		return executeUploadAction(c, v)
	case *semantic.S3DownloadAction:
		return executeDownloadAction(c, v)
	case *semantic.S3DeleteAction:
		return executeDeleteAction(c, v)
	case *semantic.S3ListAction:
		return executeListAction(c, v)
	default:
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Unsupported action type: %T", v))
	}
}

// executeUploadAction handles file upload to S3 operations
func executeUploadAction(c echo.Context, action *semantic.S3UploadAction) error {
	ctx := context.Background()
	action.StartTime = time.Now().Format(time.RFC3339)

	// Extract S3 credentials
	url, region, accessKey, secretKey, bucketName, err := semantic.ExtractS3Credentials(action.Target)
	_ = region // May be used for multi-region support
	if err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to extract S3 credentials: %v", err))
	}

	// Get file path from object
	if action.Object == nil {
		return returnError(c, action, "Object is required")
	}

	filePath := action.Object.ContentUrl
	if filePath == "" {
		return returnError(c, action, "Object contentUrl (file path) is required")
	}

	// Determine S3 key
	s3Key := action.TargetUrl
	if s3Key == "" {
		s3Key = action.Object.Identifier
	}
	if s3Key == "" {
		s3Key = filepath.Base(filePath)
	}

	// Use EVE's HetznerUploadFile function
	if err := storage.HetznerUploadFile(ctx, url, accessKey, secretKey, bucketName, filePath, s3Key); err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to upload file: %v", err))
	}

	// Get file info for result
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		action.Result = &semantic.S3Object{
			Type:           "MediaObject",
			Identifier:     s3Key,
			Name:           filepath.Base(filePath),
			ContentUrl:     fmt.Sprintf("s3://%s/%s", bucketName, s3Key),
			ContentSize:    fileInfo.Size(),
			EncodingFormat: action.Object.EncodingFormat,
			UploadDate:     time.Now().Format(time.RFC3339),
		}
	}

	action.ActionStatus = "CompletedActionStatus"
	action.EndTime = time.Now().Format(time.RFC3339)
	return c.JSON(http.StatusOK, action)
}

// executeDownloadAction handles file download from S3 operations
func executeDownloadAction(c echo.Context, action *semantic.S3DownloadAction) error {
	ctx := context.Background()
	action.StartTime = time.Now().Format(time.RFC3339)

	// Extract S3 credentials
	url, region, accessKey, secretKey, bucketName, err := semantic.ExtractS3Credentials(action.Target)
	_ = region // May be used for multi-region support
	if err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to extract S3 credentials: %v", err))
	}

	// Get S3 key from object
	if action.Object == nil {
		return returnError(c, action, "Object is required")
	}

	s3Key := action.Object.Identifier
	if s3Key == "" {
		s3Key = action.Object.Name
	}
	if s3Key == "" {
		return returnError(c, action, "Object identifier (S3 key) is required")
	}

	// Determine local download path
	downloadPath := action.Object.ContentUrl
	if downloadPath == "" {
		downloadPath = filepath.Join("/tmp", filepath.Base(s3Key))
	}

	// Create S3 client
	client, err := createS3Client(ctx, url, region, accessKey, secretKey)
	if err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to create S3 client: %v", err))
	}

	// Download file
	result, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to download file: %v", err))
	}
	defer func() { _ = result.Body.Close() }()

	// Write to local file
	outFile, err := os.Create(downloadPath)
	if err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to create local file: %v", err))
	}
	defer func() { _ = outFile.Close() }()

	size, err := io.Copy(outFile, result.Body)
	if err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to write file: %v", err))
	}

	// Set result
	action.Result = &semantic.S3Object{
		Type:           "MediaObject",
		Identifier:     s3Key,
		Name:           filepath.Base(s3Key),
		ContentUrl:     downloadPath,
		ContentSize:    size,
		EncodingFormat: action.Object.EncodingFormat,
	}

	action.ActionStatus = "CompletedActionStatus"
	action.EndTime = time.Now().Format(time.RFC3339)
	return c.JSON(http.StatusOK, action)
}

// executeDeleteAction handles file deletion from S3 operations
func executeDeleteAction(c echo.Context, action *semantic.S3DeleteAction) error {
	ctx := context.Background()
	action.StartTime = time.Now().Format(time.RFC3339)

	// Extract S3 credentials
	url, region, accessKey, secretKey, bucketName, err := semantic.ExtractS3Credentials(action.Target)
	_ = region // May be used for multi-region support
	if err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to extract S3 credentials: %v", err))
	}

	// Get S3 key from object
	if action.Object == nil {
		return returnError(c, action, "Object is required")
	}

	s3Key := action.Object.Identifier
	if s3Key == "" {
		s3Key = action.Object.Name
	}
	if s3Key == "" {
		return returnError(c, action, "Object identifier (S3 key) is required")
	}

	// Create S3 client
	client, err := createS3Client(ctx, url, region, accessKey, secretKey)
	if err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to create S3 client: %v", err))
	}

	// Delete object
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to delete file: %v", err))
	}

	action.ActionStatus = "CompletedActionStatus"
	action.EndTime = time.Now().Format(time.RFC3339)
	return c.JSON(http.StatusOK, action)
}

// executeListAction handles listing objects in S3 bucket
func executeListAction(c echo.Context, action *semantic.S3ListAction) error {
	ctx := context.Background()
	action.StartTime = time.Now().Format(time.RFC3339)

	// Extract S3 credentials
	url, region, accessKey, secretKey, bucketName, err := semantic.ExtractS3Credentials(action.Target)
	_ = region // May be used for multi-region support
	if err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to extract S3 credentials: %v", err))
	}

	// Create S3 client
	client, err := createS3Client(ctx, url, region, accessKey, secretKey)
	if err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to create S3 client: %v", err))
	}

	// List objects with optional prefix
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	}
	if action.Query != "" {
		input.Prefix = aws.String(action.Query)
	}

	result, err := client.ListObjectsV2(ctx, input)
	if err != nil {
		return returnError(c, action, fmt.Sprintf("Failed to list objects: %v", err))
	}

	// Build result list
	objects := make([]*semantic.S3Object, 0, len(result.Contents))
	for _, obj := range result.Contents {
		objects = append(objects, &semantic.S3Object{
			Type:        "MediaObject",
			Identifier:  *obj.Key,
			Name:        filepath.Base(*obj.Key),
			ContentUrl:  fmt.Sprintf("s3://%s/%s", bucketName, *obj.Key),
			ContentSize: *obj.Size,
			UploadDate:  obj.LastModified.Format(time.RFC3339),
		})
	}

	action.Result = objects
	action.ActionStatus = "CompletedActionStatus"
	action.EndTime = time.Now().Format(time.RFC3339)
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

// returnError is a helper to return error responses with proper action status
func returnError(c echo.Context, action interface{}, message string) error {
	// Set error on the action based on type
	switch v := action.(type) {
	case *semantic.S3UploadAction:
		v.ActionStatus = "FailedActionStatus"
		v.Error = &semantic.PropertyValue{Type: "PropertyValue", Name: "error", Value: message}
		v.EndTime = time.Now().Format(time.RFC3339)
		return c.JSON(http.StatusInternalServerError, v)
	case *semantic.S3DownloadAction:
		v.ActionStatus = "FailedActionStatus"
		v.Error = &semantic.PropertyValue{Type: "PropertyValue", Name: "error", Value: message}
		v.EndTime = time.Now().Format(time.RFC3339)
		return c.JSON(http.StatusInternalServerError, v)
	case *semantic.S3DeleteAction:
		v.ActionStatus = "FailedActionStatus"
		v.Error = &semantic.PropertyValue{Type: "PropertyValue", Name: "error", Value: message}
		v.EndTime = time.Now().Format(time.RFC3339)
		return c.JSON(http.StatusInternalServerError, v)
	case *semantic.S3ListAction:
		v.ActionStatus = "FailedActionStatus"
		v.Error = &semantic.PropertyValue{Type: "PropertyValue", Name: "error", Value: message}
		v.EndTime = time.Now().Format(time.RFC3339)
		return c.JSON(http.StatusInternalServerError, v)
	default:
		return echo.NewHTTPError(http.StatusInternalServerError, message)
	}
}
