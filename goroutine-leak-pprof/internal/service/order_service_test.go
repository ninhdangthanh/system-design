package service_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"go.uber.org/goleak"

	"goroutine-leak-pprof/internal/client"
	"goroutine-leak-pprof/internal/service"
)

func leakyModeFromEnv() bool {
	return os.Getenv("LEAK_MODE") == "leaky"
}

func newFakeUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /inventory/{id}", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(300 * time.Millisecond):
			_, _ = w.Write([]byte(`{"product_id":"x","stock":1}`))
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("POST /payments", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"acquirer unavailable"}`))
	})
	return httptest.NewServer(mux)
}

func newOrderService(t *testing.T, upstreamURL string) *service.OrderService {
	t.Helper()
	leaky := leakyModeFromEnv()
	httpClient := client.NewHTTPClient()
	t.Cleanup(httpClient.CloseIdleConnections)

	return service.NewOrderService(
		t.Context(),
		client.NewInventoryClient(upstreamURL, httpClient),
		client.NewPaymentClient(upstreamURL, httpClient, leaky),
		50*time.Millisecond,
		leaky,
	)
}

func TestGetOrderTimeoutDoesNotLeakGoroutines(t *testing.T) {
	upstream := newFakeUpstream(t)
	defer goleak.VerifyNone(t)
	defer upstream.Close()

	orders := newOrderService(t, upstream.URL)
	for i := range 5 {
		_, err := orders.GetOrder(context.Background(), string(rune('a'+i)))
		if err == nil {
			t.Fatal("expected inventory timeout")
		}
	}
}

func TestCheckoutFailureDoesNotLeakGoroutines(t *testing.T) {
	upstream := newFakeUpstream(t)
	defer goleak.VerifyNone(t)
	defer upstream.Close()

	orders := newOrderService(t, upstream.URL)
	for range 5 {
		if err := orders.Checkout(context.Background(), service.CheckoutRequest{OrderID: "ord-1", Amount: 10}); err == nil {
			t.Fatal("expected payment failure")
		}
	}
}
