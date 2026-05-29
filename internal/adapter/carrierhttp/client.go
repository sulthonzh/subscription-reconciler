package carrierhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/port"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *Client) CheckPlan(ctx context.Context, userID string) (string, error) {
	url := fmt.Sprintf("%s/mock/carrier/plan?userId=%s", c.baseURL, userID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "api_error", nil
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "api_error", nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return "api_error", nil
	}

	if resp.StatusCode != http.StatusOK {
		return "api_error", nil
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "api_error", nil
	}

	return body.Status, nil
}

var _ port.CarrierClient = (*Client)(nil)
