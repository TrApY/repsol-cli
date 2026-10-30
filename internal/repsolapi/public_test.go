package repsolapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/httpx"
)

// testHTTP builds an httpx client backed by the test server's transport.
func testHTTP(t *testing.T, srv *httptest.Server) *httpx.Client {
	t.Helper()
	h := httpx.New("repsol-cli/test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.HTTP = srv.Client()
	return h
}

// fixtureBytes reads a fixture in the test goroutine so handlers never call
// t.Fatalf.
func fixtureBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return b
}

func writeJSONBody(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// assertQueryValues checks the decoded query values of a request.
func assertQueryValues(t *testing.T, r *http.Request, want map[string]string) {
	t.Helper()
	got := r.URL.Query()
	for key, value := range want {
		if g := got.Get(key); g != value {
			t.Errorf("query %s = %q, want %q", key, g, value)
		}
	}
}

// assertNoQueryKeys checks that the query has no key at all, not even one with
// an empty value.
func assertNoQueryKeys(t *testing.T, r *http.Request, keys ...string) {
	t.Helper()
	q := r.URL.Query()
	for _, key := range keys {
		if _, ok := q[key]; ok {
			t.Errorf("query unexpectedly contains %s", key)
		}
	}
}

func requireAppError(t *testing.T, err error, cat apperr.Category, code string) *apperr.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an %s error, got nil", cat)
	}
	var typed *apperr.Error
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not an *apperr.Error", err)
	}
	if typed.Category != cat {
		t.Errorf("category = %v, want %v (error: %v)", typed.Category, cat, err)
	}
	if typed.Code != code {
		t.Errorf("code = %q, want %q", typed.Code, code)
	}
	return typed
}

func TestAutocompleteQuery(t *testing.T) {
	body := fixtureBytes(t, "testdata/autocomplete.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/autocomplete" {
			t.Errorf("path = %s, want /v1/autocomplete", r.URL.Path)
		}
		assertQueryValues(t, r, map[string]string{
			"input":           "Calle Ñandú 3, Móstoles",
			"maxResults":      "5",
			"neutralAddress":  "true",
			"canarias":        "true",
			"islandToExclude": "elhierro,lagomera,lapalma,fuerteventura,lanzarote,lagraciosa",
		})
		writeJSONBody(w, body)
	}))
	defer srv.Close()

	places := &PlacesClient{BaseURL: srv.URL, HTTP: testHTTP(t, srv)}
	results, err := places.Autocomplete(context.Background(), "Calle Ñandú 3, Móstoles")
	if err != nil {
		t.Fatalf("Autocomplete: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Origin != "repsol" {
		t.Errorf("results[0].Origin = %q, want repsol", results[0].Origin)
	}
	if results[1].Origin != "google" {
		t.Errorf("results[1].Origin = %q, want google", results[1].Origin)
	}
}

func TestGeocodeQuery(t *testing.T) {
	body := fixtureBytes(t, "testdata/geocode.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/geocode" {
			t.Errorf("path = %s, want /v1/geocode", r.URL.Path)
		}
		assertQueryValues(t, r, map[string]string{
			"placeId":         "fixture-place-google",
			"number":          "3",
			"canarias":        "true",
			"islandToExclude": "elhierro,lagomera,lapalma,fuerteventura,lanzarote,lagraciosa",
		})
		writeJSONBody(w, body)
	}))
	defer srv.Close()

	places := &PlacesClient{BaseURL: srv.URL, HTTP: testHTTP(t, srv)}
	addr, err := places.Geocode(context.Background(), "fixture-place-google", "3")
	if err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	if addr.Origin != "google" {
		t.Errorf("Origin = %q, want google", addr.Origin)
	}
	if addr.Latitude != FlexNumber("40.4168") {
		t.Errorf("Latitude = %q, want 40.4168", addr.Latitude)
	}
	if addr.Confidence == nil || addr.Confidence.ServiceNotFound != true {
		t.Errorf("Confidence = %#v, want service_not_found true", addr.Confidence)
	}
}

func TestAgencyQueryOmitsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agency" {
			t.Errorf("path = %s, want /agency", r.URL.Path)
		}
		assertQueryValues(t, r, map[string]string{
			"origin":             "GOOGLE",
			"inferredAgencyCode": "0000000001",
			"number":             "5",
		})
		assertNoQueryKeys(t, r, "streetCode")
		writeJSONBody(w, []byte(`{"agencyCode":"0000000001","onlinePayment":true,"wayletPayment":true,"allowsTip":true}`))
	}))
	defer srv.Close()

	a := Address{
		Origin:           "google",
		ProvinceCode:     "28",
		LocalityCode:     "28000000001",
		MunicipalityCode: "28000000001",
		Number:           "5",
		AgencyCode:       "0000000001",
	}
	c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv)}
	info, err := c.Agency(context.Background(), a)
	if err != nil {
		t.Fatalf("Agency: %v", err)
	}
	if info.AgencyCode != "0000000001" || !info.OnlinePayment || !info.WayletPayment || !info.AllowsTip {
		t.Errorf("AgencyInfo = %#v", info)
	}
}

func TestCatalogNullMaterial(t *testing.T) {
	body := fixtureBytes(t, "testdata/catalog.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/catalog" {
			t.Errorf("path = %s, want /catalog", r.URL.Path)
		}
		assertQueryValues(t, r, map[string]string{"agencyCode": "0000000001"})
		writeJSONBody(w, body)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv)}
	products, err := c.Catalog(context.Background(), "0000000001")
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	var packaging, butano, propano *Product
	for i := range products {
		switch products[i].MaterialID {
		case "":
			packaging = &products[i]
		case "153":
			butano = &products[i]
		case "239":
			propano = &products[i]
		}
	}
	if packaging == nil {
		t.Fatal("no packaging row with an empty MaterialID")
	}
	if packaging.MaterialID != "" {
		t.Errorf("packaging MaterialID = %q, want empty", packaging.MaterialID)
	}
	if butano == nil || butano.Price != 18.89 {
		t.Errorf("material 153 = %#v, want price 18.89", butano)
	}
	if propano == nil || propano.Price != 18.89 {
		t.Errorf("material 239 = %#v, want price 18.89", propano)
	}
}

func TestScheduleOptionalMode(t *testing.T) {
	body := fixtureBytes(t, "testdata/schedule.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/deliverySchedule" {
			t.Errorf("path = %s, want /deliverySchedule", r.URL.Path)
		}
		assertQueryValues(t, r, map[string]string{
			"materialId":       "153",
			"agencyCode":       "0000000001",
			"provinceCode":     "28",
			"municipalityCode": "28000000001",
			"number":           "3",
		})
		assertNoQueryKeys(t, r, "localityCode", "streetTypeCode", "streetCode")
		writeJSONBody(w, body)
	}))
	defer srv.Close()

	a := Address{
		AgencyCode:       "0000000001",
		ProvinceCode:     "28",
		MunicipalityCode: "28000000001",
		Number:           "3",
	}
	c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv)}
	schedules, err := c.DeliverySchedule(context.Background(), "153", a)
	if err != nil {
		t.Fatalf("DeliverySchedule: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("got %d schedules, want 1", len(schedules))
	}
	if schedules[0].Mode != "" {
		t.Errorf("Mode = %q, want empty", schedules[0].Mode)
	}
	if schedules[0].MaterialID != "153" {
		t.Errorf("MaterialID = %q, want 153", schedules[0].MaterialID)
	}
	if len(schedules[0].Dates) != 2 || schedules[0].Dates[0].ID != "2" || schedules[0].Dates[0].Date != "2026-11-04" {
		t.Errorf("Dates = %#v", schedules[0].Dates)
	}
	if schedules[0].Type != "WEEKLY" || schedules[0].Warning {
		t.Errorf("Type = %q, Warning = %v", schedules[0].Type, schedules[0].Warning)
	}
}

func TestBodyErrorIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONBody(w, []byte(`{"type":"EmptyOrderError","errorCode":"EMPTY_ORDER_ERROR","error":true,"message":"Empty orders"}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv)}
	_, err := c.Catalog(context.Background(), "0000000001")
	e := requireAppError(t, err, apperr.Refused, "api_error")
	if !strings.Contains(e.Message, "EMPTY_ORDER_ERROR") {
		t.Errorf("message %q does not contain EMPTY_ORDER_ERROR", e.Message)
	}
	if !strings.Contains(e.Message, "Empty orders") {
		t.Errorf("message %q does not contain the server message", e.Message)
	}
}

func TestUnexpectedShapeIsAPIChanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONBody(w, []byte(`{"foo":1}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv)}
	_, err := c.Catalog(context.Background(), "0000000001")
	requireAppError(t, err, apperr.APIChanged, "unexpected_response")
}

func TestAddressMapsEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/address-maps" {
			t.Errorf("path = %s, want /address-maps", r.URL.Path)
		}
		assertQueryValues(t, r, map[string]string{
			"province":       "Madrid",
			"provinceCode":   "28",
			"locality":       "Móstoles",
			"streetType":     "Calle",
			"streetTypeCode": "1",
			"street":         "Ñandú",
			"municipality":   "Móstoles",
			"postalCode":     "28931",
		})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := Address{
		Province:       "Madrid",
		ProvinceCode:   "28",
		Locality:       "Móstoles",
		StreetType:     "Calle",
		StreetTypeCode: "1",
		Street:         "Ñandú",
		Municipality:   "Móstoles",
		PostalCode:     "28931",
	}
	c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv)}
	got, err := c.AddressMaps(context.Background(), a)
	if err != nil {
		t.Fatalf("AddressMaps: %v", err)
	}
	if !reflect.DeepEqual(got, Address{}) {
		t.Errorf("AddressMaps = %#v, want the zero Address", got)
	}
}
