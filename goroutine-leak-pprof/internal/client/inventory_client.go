package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type InventoryClient struct {
	baseURL string
	http    *http.Client
}

type stockResponse struct {
	ProductID string `json:"product_id"`
	Stock     int    `json:"stock"`
}

func NewInventoryClient(baseURL string, httpClient *http.Client) *InventoryClient {
	return &InventoryClient{baseURL: baseURL, http: httpClient}
}

func (c *InventoryClient) GetStock(ctx context.Context, productID string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/inventory/"+productID, nil)
	if err != nil {
		return 0, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("inventory: unexpected status %d", resp.StatusCode)
	}

	var payload stockResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	return payload.Stock, nil
}
