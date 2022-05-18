package auth

import (
	// Std
	"bytes"
	"encoding/json"
	"net/http"

	// Momentum
	"github.com/momentum-xyz/controller/internal/logger"
)

var log = logger.L()

func VerifyToken(token string, introspectUrl string) bool {
	type introPayload struct {
		Token string `json:"token"`
	}
	client := &http.Client{}
	bearer := "Bearer " + token

	payload := introPayload{Token: token}
	payloadBody, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", introspectUrl, bytes.NewBuffer(payloadBody))
	req.Header.Add("Authorization", bearer)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)

	if err != nil {
		log.Warnf("token validation failed: %s", err.Error())
		return false
	}
	if resp.StatusCode != 200 {
		log.Warnf("token validation failed: %s", resp.Status)
		return false
	}
	defer resp.Body.Close()

	ret := make([]byte, resp.ContentLength)
	resp.Body.Read(ret)
	if string(ret) != "1" {
		log.Warnf("token validity check failed: %s", ret)
		return false
	}

	return true
}
