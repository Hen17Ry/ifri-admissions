package storage

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Readiness checks MinIO availability only. No upload or S3 credentials here.
func Readiness(endpoint string) func(context.Context) error {
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/minio/health/ready", nil)
		if err != nil {
			return err
		}
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("storage unavailable")
		}
		return nil
	}
}
