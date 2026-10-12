//go:build integration

package storage

import (
	"bytes"
	"context"
	"fmt"
	"ifri-admissions/services/api/internal/config"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestS3PrivateRoundtrip(t *testing.T) {
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := NewS3(c.S3Endpoint, c.S3Bucket, c.S3Region, c.S3AccessKey, c.S3SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("applications/storage-test/%d.pdf", time.Now().UnixNano())
	defer s.Delete(context.WithoutCancel(ctx), key)
	data := []byte("%PDF-1.4 synthetic storage test")
	if err = s.Put(ctx, key, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	object, err := s.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(object.Body)
	object.Body.Close()
	if err != nil || !bytes.Equal(got, data) || object.Size != int64(len(data)) || object.ContentType != "application/pdf" {
		t.Fatal("S3 metadata/body mismatch")
	}
	request, _ := http.NewRequestWithContext(ctx, "GET", c.S3Endpoint+"/"+c.S3Bucket+"/"+key, nil)
	res, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("object is public")
	}
	if err = s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if obj, err := s.Get(ctx, key); err == nil {
		obj.Body.Close()
		t.Fatal("deleted object remains")
	}
}
