// Package storage provides cloud storage implementations.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/pkg/errors"
)

// R2Service provides Cloudflare R2 storage operations
type R2Service struct {
	client       *s3.Client
	bucketName   string
	publicDomain string
}

// NewR2Service creates a new R2Service instance
func NewR2Service() (*R2Service, error) {
	// Get configuration from environment
	endpoint := os.Getenv("R2_ENDPOINT")
	bucketName := os.Getenv("R2_BUCKET_NAME")
	publicDomain := os.Getenv("R2_PUBLIC_DOMAIN")
	accessKeyID := os.Getenv("R2_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("R2_SECRET_ACCESS_KEY")

	if endpoint == "" || bucketName == "" || publicDomain == "" {
		return nil, errors.InvalidRequest("R2 configuration incomplete", nil)
	}

	// Create custom endpoint resolver for R2
	//nolint:staticcheck
	customResolver := aws.EndpointResolverWithOptionsFunc(
		func(service, _region string, options ...interface{}) (aws.Endpoint, error) {
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
	cfg, err := config.LoadDefaultConfig(context.Background(),
		//nolint:staticcheck
		config.WithEndpointResolverWithOptions(customResolver),
		config.WithCredentialsProvider(
			aws.NewCredentialsCache(
				NewStaticCredentialsProvider(accessKeyID, secretAccessKey),
			),
		),
		config.WithRegion("auto"),
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
	client := s3.NewFromConfig(cfg)

	return &R2Service{
		client:       client,
		bucketName:   bucketName,
		publicDomain: publicDomain,
	}, nil
}

// Upload uploads a file to R2 storage
func (r *R2Service) Upload(ctx context.Context, req *domain.UploadImageRequest) (storageKey, publicURL string, err error) {
	// Generate storage key: images/{userID}/{uuid}/{filename}
	storageKey = fmt.Sprintf("images/%d/%s", req.UserID, req.FileName)

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
