package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

var ErrPaymentDeclined = errors.New("payment declined by gateway")

type PaymentClient struct {
	baseURL string
	http    *http.Client
	leaky   bool
}

type chargeRequest struct {
	OrderID string  `json:"order_id"`
	Amount  float64 `json:"amount"`
}

func NewPaymentClient(baseURL string, httpClient *http.Client, leaky bool) *PaymentClient {
	return &PaymentClient{baseURL: baseURL, http: httpClient, leaky: leaky}
}

func (c *PaymentClient) Charge(ctx context.Context, orderID string, amount float64) error {
	payload, err := json.Marshal(chargeRequest{OrderID: orderID, Amount: amount})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/payments", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}

	if c.leaky {
		return handleChargeResponseLeaky(resp)
	}
	return handleChargeResponse(resp)
}

func handleChargeResponseLeaky(resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrPaymentDeclined, resp.StatusCode)
	}
	defer resp.Body.Close()
	return nil
}

func handleChargeResponse(resp *http.Response) error {
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrPaymentDeclined, resp.StatusCode)
	}
	return nil
}
