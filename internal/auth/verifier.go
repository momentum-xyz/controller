package auth

import (
	"context"
	"github.com/pkg/errors"
	"net/http"
	"sync"
	"time"
)

// NOTE: get from config?
const verifierTimeout = 5 * time.Second

type TokenVerifier interface {
	Verify(token string) error
}

func NewTokenVerifier(introspectUrl string) TokenVerifier {
	return &verifier{
		client:        &http.Client{},
		introspectUrl: introspectUrl,
	}
}

type verifier struct {
	mu            sync.Mutex
	client        *http.Client
	introspectUrl string
}

func (v *verifier) Verify(token string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), verifierTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.introspectUrl, nil)
	if err != nil {
		return errors.WithMessage(err, "failed to create request")
	}
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return errors.WithMessage(err, "failed to do request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.Errorf("invalid status code: %s", resp.Status)
	}

	// if we have 200, we should be good to go
	return nil
}
