package subscription

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/kongken/clash-gen/internal/config"
)

const YAMLContentType = "application/yaml; charset=utf-8"

// S3Objects stores published YAML in a provisioned private bucket.
type S3Objects struct {
	client *s3.Client
	bucket string
	prefix string
}

func NewS3Objects(cfg config.ObjectStoreConfig) (*S3Objects, error) {
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil || (endpoint.Scheme != "https" && endpoint.Scheme != "http") ||
		endpoint.Hostname() == "" || endpoint.User != nil || endpoint.ForceQuery ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("object_store.endpoint must be an absolute HTTP or HTTPS URL without credentials, query or fragment")
	}
	if strings.TrimSpace(cfg.Bucket) == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("object_store requires a private bucket, access key ID and secret access key")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	client := s3.New(s3.Options{
		Region:                     region,
		BaseEndpoint:               aws.String(endpoint.String()),
		UsePathStyle:               cfg.PathStyle == nil || *cfg.PathStyle,
		Credentials:                credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return &S3Objects{client: client, bucket: cfg.Bucket, prefix: prefix}, nil
}

func (s *S3Objects) put(ctx context.Context, key string, yaml []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key),
		Body: bytes.NewReader(yaml), ContentType: aws.String(YAMLContentType),
	})
	return err
}

func (s *S3Objects) get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	object, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, 0, err
	}
	length := int64(-1)
	if object.ContentLength != nil {
		length = *object.ContentLength
	}
	return object.Body, length, nil
}

func (s *S3Objects) delete(ctx context.Context, key string) error {
	object := s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}
	deleted, err := s.client.DeleteObject(ctx, &object)
	if err != nil {
		return err
	}
	if !aws.ToBool(deleted.DeleteMarker) && aws.ToString(deleted.VersionId) == "" {
		return nil
	}

	// Deleting by key in a versioned bucket hides the old bytes behind a
	// marker. Purge the exact key, including versions from retried uploads.
	pages := s3.NewListObjectVersionsPaginator(s.client, &s3.ListObjectVersionsInput{
		Bucket: object.Bucket, Prefix: object.Key,
	})
	var versions, markers []*string
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, version := range page.Versions {
			if aws.ToString(version.Key) == key {
				versions = append(versions, version.VersionId)
			}
		}
		for _, marker := range page.DeleteMarkers {
			if aws.ToString(marker.Key) == key {
				markers = append(markers, marker.VersionId)
			}
		}
	}
	// Keep the delete marker in place until all stored bytes are gone.
	for _, versionID := range append(versions, markers...) {
		if aws.ToString(versionID) == "" {
			return errors.New("object storage did not return a version ID")
		}
		object.VersionId = versionID
		if _, err := s.client.DeleteObject(ctx, &object); err != nil {
			return err
		}
	}
	return nil
}
