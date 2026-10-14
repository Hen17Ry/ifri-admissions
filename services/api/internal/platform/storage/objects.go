package storage

import (
	"context"
	"io"
	"time"
)

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}
type Object struct {
	Body        ReadSeekCloser
	Size        int64
	ModifiedAt  time.Time
	ContentType string
}

// Objects is the application port. No provider SDK type crosses this boundary.
type Objects interface {
	Put(context.Context, string, io.Reader, int64) error
	Get(context.Context, string) (Object, error)
	Delete(context.Context, string) error
}

// TypedObjects also writes non-PDF private objects (OCR proof images).
type TypedObjects interface {
	Get(context.Context, string) (Object, error)
	PutContent(context.Context, string, io.Reader, int64, string) error
}
