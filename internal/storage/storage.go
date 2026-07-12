package storage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// ReportStorage abstracts object storage for appraisal reports. The real backend
// is S3 (Yandex Cloud); StubStorage stands in until the SDK/credentials are wired.
// Hiding it behind an interface means the service never changes when we swap it.
type ReportStorage interface {
	// KeyFor builds the object key for the report of an appraisal.
	KeyFor(appraisalID uuid.UUID, filename string) string
	// PresignUpload returns a URL the client can PUT the report bytes to.
	PresignUpload(ctx context.Context, key string) (string, error)
}

// StubStorage produces deterministic keys and a fake upload URL. No network I/O.
type StubStorage struct {
	endpoint string
	bucket   string
}

func NewStubStorage(endpoint, bucket string) *StubStorage {
	return &StubStorage{endpoint: endpoint, bucket: bucket}
}

// KeyFor namespaces objects per appraisal and adds a random prefix so two files
// with the same name never collide: appraisals/<id>/<random>-<filename>.
func (s *StubStorage) KeyFor(appraisalID uuid.UUID, filename string) string {
	return fmt.Sprintf("appraisals/%s/%s-%s", appraisalID, uuid.NewString(), filename)
}

func (s *StubStorage) PresignUpload(_ context.Context, key string) (string, error) {
	return fmt.Sprintf("%s/%s/%s?stub-presigned=true", s.endpoint, s.bucket, key), nil
}
