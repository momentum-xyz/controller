package auth

import (
	"context"
	"net/http"

	"github.com/pkg/errors"
)

// VerifyToken deprecated
func VerifyToken(token string, introspectUrl string) error {
	client := &http.Client{}

	ctx, cancel := context.WithTimeout(context.Background(), verifierTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, introspectUrl, nil)
	if err != nil {
		return errors.WithMessage(err, "failed to create request")
	}
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
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
