package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"goroutine-leak-pprof/internal/client"
	"goroutine-leak-pprof/internal/service"
)

type OrderHandler struct {
	orders *service.OrderService
}

func NewOrderHandler(orders *service.OrderService) *OrderHandler {
	return &OrderHandler{orders: orders}
}

func (h *OrderHandler) GetOrder(c *gin.Context) {
	order, err := h.orders.GetOrder(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, order)
}

func (h *OrderHandler) Checkout(c *gin.Context) {
	var req service.CheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.orders.Checkout(c.Request.Context(), req); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"order_id": req.OrderID, "status": "paid"})
}

func respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInventoryTimeout):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": err.Error()})
	case errors.Is(err, client.ErrPaymentDeclined):
		c.JSON(http.StatusPaymentRequired, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
	}
}
