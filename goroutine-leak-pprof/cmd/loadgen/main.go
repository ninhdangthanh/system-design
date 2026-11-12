package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type requestFactory func(baseURL string, seq int64) (*http.Request, error)

var scenarios = map[string][]requestFactory{
	"orders":   {getOrder},
	"checkout": {checkout},
	"reports":  {salesReport},
	"all":      {getOrder, checkout, salesReport},
}

type stats struct {
	mu           sync.Mutex
	statusCounts map[string]int
	totalLatency time.Duration
	requests     int
}

func main() {
	baseURL := flag.String("base", "http://localhost:8080", "API base URL")
	scenario := flag.String("scenario", "all", "orders | checkout | reports | all")
	concurrency := flag.Int("c", 20, "concurrent workers")
	duration := flag.Duration("d", 30*time.Second, "test duration")
	flag.Parse()

	factories, ok := scenarios[*scenario]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown scenario %q\n", *scenario)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *duration)
	defer cancel()

	fmt.Printf("load: scenario=%s workers=%d duration=%s base=%s\n", *scenario, *concurrency, *duration, *baseURL)

	result := &stats{statusCounts: make(map[string]int)}
	httpClient := &http.Client{Timeout: 10 * time.Second}
	var seq atomic.Int64
	started := time.Now()

	var wg sync.WaitGroup
	for range *concurrency {
		wg.Go(func() {
			for ctx.Err() == nil {
				n := seq.Add(1)
				factory := factories[int(n)%len(factories)]
				result.record(send(httpClient, factory, *baseURL, n))
			}
		})
	}

	go reportProgress(ctx, result)
	wg.Wait()
	result.print(time.Since(started))
}

type outcome struct {
	status  string
	latency time.Duration
}

func send(httpClient *http.Client, factory requestFactory, baseURL string, seq int64) outcome {
	req, err := factory(baseURL, seq)
	if err != nil {
		return outcome{status: "build_error"}
	}
	started := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return outcome{status: "conn_error", latency: time.Since(started)}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return outcome{status: fmt.Sprintf("%d", resp.StatusCode), latency: time.Since(started)}
}

func (s *stats) record(o outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusCounts[o.status]++
	s.totalLatency += o.latency
	s.requests++
}

func (s *stats) snapshot() (int, map[string]int, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[string]int, len(s.statusCounts))
	for k, v := range s.statusCounts {
		counts[k] = v
	}
	return s.requests, counts, s.totalLatency
}

func (s *stats) print(elapsed time.Duration) {
	requests, counts, totalLatency := s.snapshot()
	fmt.Println("---- summary ----")
	fmt.Printf("requests: %d  rps: %.1f\n", requests, float64(requests)/elapsed.Seconds())
	if requests > 0 {
		fmt.Printf("avg latency: %s\n", (totalLatency / time.Duration(requests)).Round(time.Millisecond))
	}
	fmt.Printf("status: %s\n", formatCounts(counts))
}

func reportProgress(ctx context.Context, s *stats) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			requests, counts, _ := s.snapshot()
			fmt.Printf("  sent=%d status=%s\n", requests, formatCounts(counts))
		}
	}
}

func formatCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	for _, k := range keys {
		fmt.Fprintf(&buf, "%s=%d ", k, counts[k])
	}
	return buf.String()
}

func getOrder(baseURL string, seq int64) (*http.Request, error) {
	return http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/orders/%d", baseURL, seq), nil)
}

func checkout(baseURL string, seq int64) (*http.Request, error) {
	body := fmt.Sprintf(`{"order_id":"ord-%d","amount":%d.99}`, seq, 10+seq%90)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/orders/checkout", bytes.NewBufferString(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func salesReport(baseURL string, seq int64) (*http.Request, error) {
	day := fmt.Sprintf("2026-11-%02d", 5+seq%3)
	return http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/reports/sales?day=%s&_ts=%d", baseURL, day, time.Now().UnixNano()), nil)
}
