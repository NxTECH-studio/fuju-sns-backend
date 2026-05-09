// Package storage provides cloud storage implementations.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	appconfig "github.com/fuju/backend/config"
	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/pkg/errors"
	"github.com/oklog/ulid/v2"
)

// R2Service provides Cloudflare R2 storage operations
type R2Service struct {
	client       *s3.Client
	bucketName   string
	publicDomain string
}

// NewR2Service creates a new R2Service instance from the application
// configuration. Caller must have verified cfg.R2Enabled() before
// invoking — passing a partially-populated config returns an error so
// the misconfiguration is visible at boot.
func NewR2Service(cfg *appconfig.Config) (*R2Service, error) {
	if cfg == nil {
		return nil, errors.InvalidRequest("config is required", nil)
	}
	if !cfg.R2Enabled() {
		return nil, errors.InvalidRequest("R2 configuration incomplete", nil)
	}

	endpoint := cfg.R2Endpoint
	bucketName := cfg.R2BucketName
	publicDomain := cfg.R2PublicDomain
	accessKeyID := cfg.R2AccessKeyID
	secretAccessKey := cfg.R2SecretAccessKey

	// Create custom endpoint resolver for R2
	//nolint:staticcheck
	customResolver := aws.EndpointResolverWithOptionsFunc(
		func(service, _ string, _ ...interface{}) (aws.Endpoint, error) {
			if service == s3.ServiceID {
				//nolint:staticcheck
				return aws.Endpoint{
					URL:           endpoint,
					SigningRegion: "auto",
				}, nil
			}
			//nolint:staticcheck
			return aws.Endpoint{}, fmt.Errorf("unknown service")
		},
	)

	// Load AWS configuration with custom credentials and endpoint
	//nolint:staticcheck
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		//nolint:staticcheck
		awsconfig.WithEndpointResolverWithOptions(customResolver),
		awsconfig.WithCredentialsProvider(
			aws.NewCredentialsCache(
				NewStaticCredentialsProvider(accessKeyID, secretAccessKey),
			),
		),
		awsconfig.WithRegion("auto"),
	)
	if err != nil {
		return nil, errors.New(
			errors.ErrExternalService,
			"failed to load AWS config",
			500,
			err,
		)
	}

	// Create S3 client
	client := s3.NewFromConfig(awsCfg)

	return &R2Service{
		client:       client,
		bucketName:   bucketName,
		publicDomain: publicDomain,
	}, nil
}

// Upload uploads a file to R2 storage. The storage key is
// images/{userID}/{ulid}/{safeFilename} where safeFilename is the submitted
// filename with any directory components stripped, guaranteeing the object
// always lands under the caller's own prefix even for malicious input.
func (r *R2Service) Upload(ctx context.Context, req *domain.UploadImageRequest) (storageKey, publicURL string, err error) {
	objectID := ulid.Make().String()
	safeName := sanitizeFilename(req.FileName)
	storageKey = fmt.Sprintf("images/%s/%s/%s", req.UserID, objectID, safeName)

	// Upload to R2
	_, err = r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r.bucketName),
		Key:         aws.String(storageKey),
		Body:        bytes.NewReader(req.FileData),
		ContentType: aws.String(req.MimeType),
	})

	if err != nil {
		return "", "", errors.New(
			errors.ErrExternalService,
			"failed to upload file to R2",
			500,
			err,
		)
	}

	publicURL = fmt.Sprintf("%s/%s", r.publicDomain, storageKey)
	return storageKey, publicURL, nil
}

// sanitizeFilename strips any directory components from a user-supplied
// filename so it cannot escape its intended prefix. Falls back to "file" if
// the result is empty.
func sanitizeFilename(name string) string {
	// Handle both unix and windows path separators before filepath.Base so
	// neither can smuggle through on the opposite platform.
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	if name == "" || name == "." || name == "/" {
		return "file"
	}
	return name
}

// Delete deletes a file from R2 storage
func (r *R2Service) Delete(ctx context.Context, storageKey string) error {
	_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r.bucketName),
		Key:    aws.String(storageKey),
	})

	if err != nil {
		return errors.New(
			errors.ErrExternalService,
			"failed to delete file from R2",
			500,
			err,
		)
	}

	return nil
}

// GetPublicURL returns the public URL for a storage key
func (r *R2Service) GetPublicURL(storageKey string) string {
	return fmt.Sprintf("%s/%s", r.publicDomain, storageKey)
}

// Helper types for AWS SDK compatibility

// StaticCredentialsProvider provides static AWS credentials
type StaticCredentialsProvider struct {
	accessKeyID     string
	secretAccessKey string
}

// NewStaticCredentialsProvider creates a new StaticCredentialsProvider
func NewStaticCredentialsProvider(accessKeyID, secretAccessKey string) *StaticCredentialsProvider {
	return &StaticCredentialsProvider{
		accessKeyID:     accessKeyID,
		secretAccessKey: secretAccessKey,
	}
}

// Retrieve returns the credentials
func (p *StaticCredentialsProvider) Retrieve(_ context.Context) (aws.Credentials, error) {
	return aws.Credentials{
		AccessKeyID:     p.accessKeyID,
		SecretAccessKey: p.secretAccessKey,
	}, nil
}
