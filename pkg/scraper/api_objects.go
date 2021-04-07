package scraper

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

type authParams struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (ap *authParams) validate() error {
	if ap.Password == "" {
		return fmt.Errorf("cybera api password cannot be empty")
	}
	if ap.Username == "" {
		return fmt.Errorf("cybera api username cannot be empty")
	}
	return nil
}

type siteItem struct {
	Name            string          `json:"name"`
	PhoneNumber     string          `phoneNumber:"name"`
	PhysicalAddress physicalAddress `json:"physicalAddress"`
	Status          string          `json:"status"`
}

type physicalAddress struct {
	City    string  `json:"city"`
	Country string  `country:"name"`
	ID      int     `json:"id"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	State   string  `json:"state"`
}

type idsByStatus struct {
	ID int64
}

func parseAPIResponse(data []byte) ([]*siteItem, error) {
	var resp []*siteItem
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("cannot parse api response, err: %w", err)
	}
	return resp, nil
}

func parseIDAPIResponse(data []byte) (*siteItem, error) {
	var resp siteItem
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("cannot parse api response, err: %w", err)
	}
	return &resp, nil
}

func parseIDsResponseByStatus(data []byte) ([]int64, error) {
	var parsed []idsByStatus
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	ids := make([]int64, len(parsed))
	for i := range parsed {
		ids[i] = parsed[i].ID
	}
	return ids, nil
}

// jwtClaims - supported jwt claims.
type jwtClaims struct {
	Exp int64 `json:"exp"`
}

// getExpireTimeFromJWT extracts expiration time from given jwt token.
func getExpireTimeFromJWT(token []byte) (*time.Time, error) {
	jwt := bytes.Split(token, []byte("."))
	if len(jwt) != 3 {
		return nil, fmt.Errorf("bad jwt token format, expected 3 dot delimited parts, got: %v", string(token))
	}
	claim := jwt[1]
	claimS, err := base64.RawStdEncoding.DecodeString(string(claim))
	if err != nil {
		return nil, fmt.Errorf("cannot base64 decode jwt token: %w", err)
	}
	var jc jwtClaims
	if err := json.Unmarshal(claimS, &jc); err != nil {
		return nil, fmt.Errorf("cannot parse jwt claim: %w", err)
	}
	t := time.Unix(jc.Exp, 0)
	return &t, nil
}
