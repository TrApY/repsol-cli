package fakerepsol

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// WayletHost is the host of the payment URLs the fake hands out. It is never
// resolved: the fake serves nothing there.
const WayletHost = "waylet.example.test"

// payment is one temporary order waiting for, or already past, its Waylet
// payment.
type payment struct {
	req        placedRequest
	externalID string
	confirmed  bool
	polls      int
}

// SetWayletPayment sets whether the agency accepts Waylet payments. The zero
// state of a new fake is true.
func (s *Server) SetWayletPayment(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waylet = enabled
}

// SetWayletPayAfter makes a payment confirm itself on the n-th lookup of its
// order. It models the owner paying while the CLI waits. Zero, the default,
// means a payment never confirms on its own; ConfirmWaylet pays it explicitly.
func (s *Server) SetWayletPayAfter(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wayletPayAfter = n
}

// PaymentIDs returns the ids of the temporary orders created so far, in order.
func (s *Server) PaymentIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paymentOrder...)
}

// ConfirmWaylet pays one temporary order: it prepends the order to the history
// and makes the payment lookup answer it. It reports whether the payment id
// exists and was not paid yet.
func (s *Server) ConfirmWaylet(paymentID string) bool {
	s.mu.Lock()
	p, ok := s.payments[paymentID]
	if !ok || p.confirmed {
		s.mu.Unlock()
		return false
	}
	p.confirmed = true
	s.confirmedCount++
	p.externalID = fmt.Sprintf("80000000000000%04d", 2+s.confirmedCount)
	req := p.req
	s.mu.Unlock()

	s.prependHistory(req)
	return true
}

// handleCart saves a temporary order and answers its payment id.
func (s *Server) handleCart(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(w, r) {
		return
	}
	var req placedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   true,
			"message": "the fake could not decode the request",
		})
		return
	}

	s.mu.Lock()
	id := fmt.Sprintf("fake-payment-%d", len(s.paymentOrder)+1)
	s.payments[id] = &payment{req: req}
	s.paymentOrder = append(s.paymentOrder, id)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"paymentId": id})
}

// handleWayletURL answers the page that pays one temporary order.
func (s *Server) handleWayletURL(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(w, r) {
		return
	}
	var req struct {
		PaymentID string `json:"paymentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	_, known := s.payments[req.PaymentID]
	s.mu.Unlock()
	if !known {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"wayletRedirectionPaymentUrl": "https://" + WayletHost + "/pay?paymentId=" + url.QueryEscape(req.PaymentID),
	})
}

// handleOrderByPayment answers the order of a payment once it is confirmed, and
// 404 until then.
func (s *Server) handleOrderByPayment(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(w, r) {
		return
	}
	id := r.URL.Query().Get("paymentId")

	s.mu.Lock()
	p, known := s.payments[id]
	if known {
		p.polls++
	}
	payNow := known && !p.confirmed && s.wayletPayAfter > 0 && p.polls >= s.wayletPayAfter
	s.mu.Unlock()

	if payNow {
		s.ConfirmWaylet(id)
	}

	s.mu.Lock()
	if !known || !p.confirmed {
		s.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	req, externalID := p.req, p.externalID
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"externalId":    externalID,
		"deliveryDate":  req.DeliveryDate,
		"materialId":    req.MaterialID,
		"quantity":      req.Quantity,
		"unitPrice":     req.UnitPrice,
		"paymentMethod": "waylet",
		"paymentId":     id,
		"address":       req.Address,
		"agency": repsolapi.PlacedAgency{
			Name:        "Agencia Test",
			Phone:       "900 000 000",
			RepsolPhone: "900 000 001",
		},
	})
}
