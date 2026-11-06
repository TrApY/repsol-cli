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
// payment. Like Repsol's, it has two names: the id the cart answers
// (cartID) and the id Waylet pays under (lookupID), and only the second one
// finds the order once it is paid. Live, the cart answered 000001623288 and the
// order was found under W00001623288; the lookup answered 404 to the first,
// and to the id of the payment URL as well.
type payment struct {
	cartID     string
	lookupID   string
	linkID     string
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

// PaymentIDs returns the cart ids of the temporary orders created so far, in
// order. They are the ids POST /cart answered, which the payment lookup does
// not know.
func (s *Server) PaymentIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paymentOrder...)
}

// LookupID returns the id the payment lookup knows the payment of a cart under,
// or "" when the cart is unknown. It is also what the payment URL carries in
// its token.
func (s *Server) LookupID(cartID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.payments[cartID]; ok {
		return p.lookupID
	}
	return ""
}

// ConfirmWaylet pays one temporary order: it prepends the order to the history
// and makes the payment lookup answer it. It takes either name of the payment
// (the owner pays the link, whatever the CLI calls the payment) and reports
// whether the payment exists and was not paid yet.
func (s *Server) ConfirmWaylet(paymentID string) bool {
	s.mu.Lock()
	p, ok := s.paymentByAnyID(paymentID)
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
	n := len(s.paymentOrder) + 1
	p := &payment{
		cartID:   fmt.Sprintf("fake-cart-%d", n),
		lookupID: fmt.Sprintf("fake-payment-%d", n),
		linkID:   fmt.Sprintf("fake-link-%d", n),
		req:      req,
	}
	s.payments[p.cartID] = p
	s.paymentOrder = append(s.paymentOrder, p.cartID)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"paymentId": p.cartID})
}

// paymentByAnyID finds a payment by its cart id or by its lookup id. The caller
// holds the lock.
func (s *Server) paymentByAnyID(id string) (*payment, bool) {
	if p, ok := s.payments[id]; ok {
		return p, true
	}
	for _, p := range s.payments {
		if p.lookupID == id {
			return p, true
		}
	}
	return nil, false
}

// paymentByLookupID finds a payment by the one id the payment lookup answers
// to. The caller holds the lock.
func (s *Server) paymentByLookupID(id string) (*payment, bool) {
	for _, p := range s.payments {
		if p.lookupID == id {
			return p, true
		}
	}
	return nil, false
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
	p, known := s.payments[req.PaymentID]
	s.mu.Unlock()
	if !known {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	// The shape of the live link: the Waylet id leads the token, ahead of a
	// timestamp and a signature, and a separate opaque id follows.
	token := p.lookupID + ":1796519223591:" + "c0ffee"
	writeJSON(w, http.StatusOK, map[string]any{
		"wayletRedirectionPaymentUrl": "https://" + WayletHost + "/pay?token=" + url.QueryEscape(token) +
			"&id=" + url.QueryEscape(p.linkID),
	})
}

// handleOrderByPayment answers the order of a payment once it is confirmed, and
// 404 until then. Only the lookup id finds it: the cart id and the id of the
// payment URL answer 404 even after the payment is made, as Repsol does.
func (s *Server) handleOrderByPayment(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(w, r) {
		return
	}
	id := r.URL.Query().Get("paymentId")

	s.mu.Lock()
	p, known := s.paymentByLookupID(id)
	if known {
		p.polls++
	}
	payNow := known && !p.confirmed && s.wayletPayAfter > 0 && p.polls >= s.wayletPayAfter
	s.mu.Unlock()

	if payNow {
		s.ConfirmWaylet(p.cartID)
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
