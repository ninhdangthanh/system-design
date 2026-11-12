package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrInventoryTimeout = errors.New("inventory service timed out")

const (
	trackerHistoryCapacity = 128 << 10
	trackerHeartbeat       = time.Second
)

type InventoryClient interface {
	GetStock(ctx context.Context, productID string) (int, error)
}

type PaymentClient interface {
	Charge(ctx context.Context, orderID string, amount float64) error
}

type Order struct {
	ID        string `json:"id"`
	ProductID string `json:"product_id"`
	Stock     int    `json:"stock"`
}

type CheckoutRequest struct {
	OrderID string  `json:"order_id" binding:"required"`
	Amount  float64 `json:"amount" binding:"required,gt=0"`
}

type OrderService struct {
	rootCtx          context.Context
	inventory        InventoryClient
	payment          PaymentClient
	inventoryTimeout time.Duration
	leaky            bool
}

type stockResult struct {
	stock int
	err   error
}

type orderTracker struct {
	history []byte
}

func NewOrderService(rootCtx context.Context, inventory InventoryClient, payment PaymentClient, inventoryTimeout time.Duration, leaky bool) *OrderService {
	return &OrderService{
		rootCtx:          rootCtx,
		inventory:        inventory,
		payment:          payment,
		inventoryTimeout: inventoryTimeout,
		leaky:            leaky,
	}
}

func (s *OrderService) GetOrder(ctx context.Context, orderID string) (Order, error) {
	ctx, cancel := context.WithTimeout(ctx, s.inventoryTimeout)
	defer cancel()

	productID := "sku-" + orderID
	results := s.newStockResultChannel()

	go func() {
		stock, err := s.inventory.GetStock(ctx, productID)
		results <- stockResult{stock: stock, err: err}
	}()

	select {
	case result := <-results:
		if result.err != nil {
			return Order{}, result.err
		}
		return Order{ID: orderID, ProductID: productID, Stock: result.stock}, nil
	case <-ctx.Done():
		return Order{}, fmt.Errorf("%w: %v", ErrInventoryTimeout, ctx.Err())
	}
}

func (s *OrderService) newStockResultChannel() chan stockResult {
	if s.leaky {
		return make(chan stockResult)
	}
	return make(chan stockResult, 1)
}

func (s *OrderService) Checkout(ctx context.Context, req CheckoutRequest) error {
	updates := s.startTracking(req.OrderID)
	defer s.finishTracking(updates)

	updates <- "payment_pending"

	paymentCtx := context.WithoutCancel(ctx)
	if err := s.payment.Charge(paymentCtx, req.OrderID, req.Amount); err != nil {
		updates <- "payment_failed"
		return err
	}

	updates <- "paid"
	return nil
}

func (s *OrderService) startTracking(orderID string) chan<- string {
	updates := make(chan string)
	go s.track(orderID, updates)
	return updates
}

func (s *OrderService) finishTracking(updates chan<- string) {
	if s.leaky {
		return
	}
	close(updates)
}

func (s *OrderService) track(orderID string, updates <-chan string) {
	tracker := &orderTracker{history: append(make([]byte, 0, trackerHistoryCapacity), orderID...)}
	heartbeat := time.NewTicker(trackerHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case status, ok := <-updates:
			if !ok {
				return
			}
			tracker.history = append(tracker.history, status...)
		case <-heartbeat.C:
			tracker.history = append(tracker.history, '.')
		case <-s.rootCtx.Done():
			return
		}
	}
}
