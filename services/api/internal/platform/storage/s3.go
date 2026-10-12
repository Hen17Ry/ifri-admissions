package storage

import (
	"context"
	"fmt"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"io"
	"net/http"
	"net/url"
	"time"
)

type S3 struct {
	client *minio.Client
	bucket string
}

func NewS3(endpoint, bucket, region, key, secret string) (*S3, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("S3 endpoint invalid")
	}
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(key, secret, ""), Secure: u.Scheme == "https", Region: region, BucketLookup: minio.BucketLookupPath, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 10 * time.Second, MaxIdleConnsPerHost: 5}})
	if err != nil {
		return nil, fmt.Errorf("S3 configuration invalid")
	}
	return &S3{client, bucket}, nil
}
func (s *S3) Put(ctx context.Context, key string, body io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, body, size, minio.PutObjectOptions{ContentType: "application/pdf", DisableMultipart: true})
	if err != nil {
		return fmt.Errorf("S3 put failed")
	}
	return nil
}
func (s *S3) Get(ctx context.Context, key string) (Object, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return Object{}, fmt.Errorf("S3 get failed")
	}
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return Object{}, fmt.Errorf("S3 stat failed")
	}
	return Object{obj, info.Size, info.LastModified, info.ContentType}, nil
}
func (s *S3) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("S3 delete failed")
	}
	return nil
}
