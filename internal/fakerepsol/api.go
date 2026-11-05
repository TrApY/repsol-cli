package fakerepsol

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// handleHealth reports the API as healthy.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "UP"})
}

// handleAddressMaps answers with an empty body, as the capture shows.
func (s *Server) handleAddressMaps(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleAgency returns the agency that serves the address.
func (s *Server) handleAgency(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	code := s.agency
	waylet := s.waylet
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, repsolapi.AgencyInfo{
		AgencyCode:    code,
		OnlinePayment: true,
		WayletPayment: waylet,
		AllowsTip:     true,
	})
}

// handleCatalog returns one packaging row without a material id followed by the
// orderable products.
func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]any{
		{"id": "0", "materialId": nil, "type": "Embalaje", "description": "Embalaje", "price": 0.0, "pricePerKilo": 0.0},
		{"id": "1", "materialId": "153", "type": "Butano 12.5kg", "description": "Bombona de butano de 12,5 kg", "price": 18.89, "pricePerKilo": 0.0},
		{"id": "2", "materialId": "154", "type": "Butano 12kg - Ligera", "description": "Bombona ligera de butano de 12 kg", "price": 23.4, "pricePerKilo": 0.0},
		{"id": "3", "materialId": "239", "type": "Propano 11kg", "description": "Bombona de propano de 11 kg", "price": 18.89, "pricePerKilo": 0.0},
		{"id": "4", "materialId": "210", "type": "Propano 35kg", "description": "Bombona de propano de 35 kg", "price": 88.7, "pricePerKilo": 0.0},
		{"id": "5", "materialId": "230", "type": "Autogas 12kg - Carretillas", "description": "Bombona de autogas de 12 kg", "price": 26.6, "pricePerKilo": 0.0},
	})
}

// handleDeliverySchedule offers two dates for the requested material: today+3
// with id 1 and today+5 with id 2.
func (s *Server) handleDeliverySchedule(w http.ResponseWriter, r *http.Request) {
	today := s.todayDate()
	writeJSON(w, http.StatusOK, []repsolapi.Schedule{{
		MaterialID: r.URL.Query().Get("materialId"),
		Dates: []repsolapi.DeliveryDate{
			{ID: "2", Date: today.AddDate(0, 0, 5).Format(time.DateOnly)},
			{ID: "1", Date: today.AddDate(0, 0, 3).Format(time.DateOnly)},
		},
		Mode:    "NO_SCHEDULE",
		Warning: false,
		Type:    "WEEKLY",
	}})
}

// handleOrderHistory returns the history newest first, honouring limit and
// offset.
func (s *Server) handleOrderHistory(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(w, r) {
		return
	}
	limit := queryInt(r, "limit")
	offset := queryInt(r, "offset")

	s.mu.Lock()
	history := make([]repsolapi.HistoryEntry, len(s.history))
	copy(history, s.history)
	s.mu.Unlock()

	if offset < 0 {
		offset = 0
	}
	if offset > len(history) {
		offset = len(history)
	}
	history = history[offset:]
	if limit > 0 && limit < len(history) {
		history = history[:limit]
	}
	writeJSON(w, http.StatusOK, history)
}

// handleCartCheck returns the configured cart check answer.
func (s *Server) handleCartCheck(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(w, r) {
		return
	}
	s.mu.Lock()
	check := s.check
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, check)
}

// handleSolicitude places a pay-on-delivery order.
func (s *Server) handleSolicitude(w http.ResponseWriter, r *http.Request) {
	s.place(w, r, ExternalID)
}

// handleOrder places an order.
func (s *Server) handleOrder(w http.ResponseWriter, r *http.Request) {
	s.place(w, r, orderExternalID)
}

// placedRequest is the part of both write bodies the fake echoes back.
type placedRequest struct {
	Address      repsolapi.Address `json:"address"`
	MaterialID   string            `json:"materialId"`
	Quantity     int               `json:"quantity"`
	UnitPrice    float64           `json:"unitPrice"`
	DeliveryDate string            `json:"deliveryDate"`
}

// place records one write and echoes it back as a placed order.
func (s *Server) place(w http.ResponseWriter, r *http.Request, externalID string) {
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

	s.prependHistory(req)
	writeJSON(w, http.StatusOK, repsolapi.Placed{
		ExternalID:   externalID,
		DeliveryDate: req.DeliveryDate,
		Material:     "Butano 12.5kg",
		Quantity:     req.Quantity,
		UnitPrice:    req.UnitPrice,
		Address: repsolapi.PlacedAddress{
			Province:     req.Address.Province,
			Locality:     req.Address.Locality,
			Municipality: req.Address.Municipality,
			Street:       req.Address.Street,
			Number:       req.Address.Number,
			Door:         req.Address.Door,
			Floor:        req.Address.Floor,
			PostalCode:   req.Address.PostalCode,
			StreetType:   req.Address.StreetType,
		},
		Agency: repsolapi.PlacedAgency{
			Name:        "Agencia Test",
			Phone:       "900 000 000",
			RepsolPhone: "900 000 001",
		},
	})
}

// prependHistory adds the placed order to the front of the history. Reference
// SAC numbers count from 1.
func (s *Server) prependHistory(req placedRequest) {
	createdAt := s.todayDate().Format("2006-01-02T15:04:05.000-0700")

	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	entry := repsolapi.HistoryEntry{
		CreatedAt:    createdAt,
		DeliveryDate: req.DeliveryDate,
		ReferenceSAC: fmt.Sprintf("2000000%d", s.writes),
		MaterialID:   req.MaterialID,
		Quantity:     req.Quantity,
		UnitPrice:    req.UnitPrice,
		Agency: repsolapi.HistoryAgency{
			Name:        "Agencia Test",
			Phone:       "900 000 000",
			RepsolPhone: "900 000 001",
			AgencyCode:  AgencyCode,
		},
		Address: repsolapi.HistoryAddress{
			Province:         req.Address.Province,
			ProvinceCode:     req.Address.ProvinceCode,
			Locality:         req.Address.Locality,
			LocalityCode:     req.Address.LocalityCode,
			StreetType:       req.Address.StreetType,
			Street:           req.Address.Street,
			Municipality:     req.Address.Municipality,
			MunicipalityCode: req.Address.MunicipalityCode,
			Number:           req.Address.Number,
			Floor:            req.Address.Floor,
			Door:             req.Address.Door,
			PostalCode:       req.Address.PostalCode,
		},
	}
	s.history = append([]repsolapi.HistoryEntry{entry}, s.history...)
}

// authenticated checks the signature headers every private API route needs. It
// answers 401 otherwise.
func (s *Server) authenticated(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("uid") == UID && r.Header.Get("signature") == Signature {
		return true
	}
	w.WriteHeader(http.StatusUnauthorized)
	return false
}

// queryInt reads one integer query parameter, ignoring a missing or malformed
// value.
func queryInt(r *http.Request, key string) int {
	n, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return 0
	}
	return n
}
