package httpx

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestRedactURL(t *testing.T) {
	u, err := url.Parse("https://api.example.com/api/things" +
		"?Login_Token=st2-SECRET&input=Calle%20SECRET&agencyCode=12&loginID=me%40example.com")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	got := RedactURL(u)
	for _, secret := range []string{
		"st2-SECRET", "Calle SECRET", "Calle%20SECRET", "me@example.com", "me%40example.com",
	} {
		if strings.Contains(got, secret) {
			t.Errorf("RedactURL leaked %q: %s", secret, got)
		}
	}
	for _, want := range []string{"/api/things", "agencyCode=12", "REDACTED"} {
		if !strings.Contains(got, want) {
			t.Errorf("RedactURL result missing %q: %s", want, got)
		}
	}
}

func TestRedactURLNil(t *testing.T) {
	if got := RedactURL(nil); got != "" {
		t.Errorf("RedactURL(nil) = %q, want empty", got)
	}
}

func TestRedactHeader(t *testing.T) {
	h := http.Header{
		"Cookie":         {"gmid=gmid-SECRET"},
		"Uid":            {"uid-SECRET"},
		"Signature":      {"sig-SECRET"},
		"Ptb-Gigya-User": {"uid-SECRET"},
		"Accept":         {"application/json"},
		"X-Trace":        {"a", "b"},
	}
	// A lowercase spelling must be redacted too: matching is case-insensitive.
	h["cookie"] = []string{"gmid=gmid-SECRET"}

	got := RedactHeader(h)
	for _, key := range []string{"Cookie", "Uid", "Signature", "Ptb-Gigya-User", "cookie"} {
		if got[key] != "REDACTED" {
			t.Errorf("RedactHeader[%q] = %q, want %q", key, got[key], "REDACTED")
		}
	}
	// Non-sensitive headers keep their values; several entries are joined.
	if got["Accept"] != "application/json" {
		t.Errorf("RedactHeader[Accept] = %q, want %q", got["Accept"], "application/json")
	}
	if got["X-Trace"] != "a, b" {
		t.Errorf("RedactHeader[X-Trace] = %q, want %q", got["X-Trace"], "a, b")
	}
}

func TestRedactBodyCoversSensitiveKeys(t *testing.T) {
	keys := []string{
		"login_token", "loginid", "password", "uid", "uidsignature", "signature",
		"signaturetimestamp", "usergigyauid", "ptb-gigya-user", "cookie", "set-cookie",
		"gmid", "email", "firstname", "lastname", "profile", "data", "phones", "phone",
		"name", "nif", "street", "number", "floor", "door", "details", "placeid",
		"fulladdress", "autocompletecontent", "currentfulladdress", "currentstreet",
		"latitude", "longitude", "location", "input", "externalid", "referencesac",
		"postalcode", "riskcontext", "sessioninfo", "emails", "loginids", "userinfo",
		"identities", "nickname", "uidsig", "loginprovideruid", "provideruid",
		"address", "agency", "locality", "localitycode", "municipality",
		"municipalitycode", "streetcode", "paymentid", "wayletredirectionpaymenturl",
	}

	values := url.Values{}
	for i, key := range keys {
		values.Set(key, fmt.Sprintf("secret-value-%d", i))
	}
	values.Set("agencyCode", "123")

	out := RedactBody([]byte(values.Encode()))
	for i, key := range keys {
		secret := fmt.Sprintf("secret-value-%d", i)
		if strings.Contains(out, secret) {
			t.Errorf("RedactBody leaked the value of %q: %s", key, out)
		}
	}
	if !strings.Contains(out, "agencyCode=123") {
		t.Errorf("RedactBody dropped a non-sensitive key: %s", out)
	}
	if !strings.Contains(out, "REDACTED") {
		t.Errorf("RedactBody missing the redaction marker: %s", out)
	}
}

func TestRedactBodyKeyMatchingIsCaseInsensitive(t *testing.T) {
	body := []byte("PASSWORD=pw-SECRET&Login_Token=st2-SECRET&UID=uid-SECRET&agencyCode=12")
	out := RedactBody(body)
	for _, secret := range []string{"pw-SECRET", "st2-SECRET", "uid-SECRET"} {
		if strings.Contains(out, secret) {
			t.Errorf("RedactBody leaked %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "agencyCode=12") {
		t.Errorf("RedactBody dropped a non-sensitive key: %s", out)
	}
}

func TestRedactBodyJSON(t *testing.T) {
	body := []byte(`{
		"UID": "uid-SECRET",
		"UIDSignature": "sig-SECRET",
		"profile": {"email": "me@example.com"},
		"data": {"DocID": {"Pers": "12345678Z"}},
		"phones": ["600111222", "600333444"],
		"sessionInfo": {"login_token": "st2.SECRET"},
		"address": {"street": "Calle SECRET", "number": "7"},
		"result": {"uid": "nested-SECRET", "code": "ok"},
		"errorCode": 0,
		"status": "ok"
	}`)

	out := RedactBody(body)
	for _, secret := range []string{
		"uid-SECRET", "sig-SECRET", "me@example.com", "12345678Z",
		"600111222", "600333444", "st2.SECRET", "Calle SECRET", "nested-SECRET",
	} {
		if strings.Contains(out, secret) {
			t.Errorf("RedactBody leaked %q: %s", secret, out)
		}
	}
	for _, want := range []string{
		`"UID":"REDACTED"`,
		`"UIDSignature":"REDACTED"`,
		`"profile":"REDACTED"`,
		`"data":"REDACTED"`,
		`"phones":"REDACTED"`,
		`"address":"REDACTED"`,
		`"sessionInfo":"REDACTED"`,
		`"result":{"code":"ok","uid":"REDACTED"}`,
		`"errorCode":0`,
		`"status":"ok"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RedactBody result missing %q: %s", want, out)
		}
	}
}

func TestRedactBodyJSONArray(t *testing.T) {
	body := []byte(`[{"password":"pw-SECRET","ok":1},{"login_token":"st2.SECRET"}]`)
	out := RedactBody(body)
	for _, secret := range []string{"pw-SECRET", "st2.SECRET"} {
		if strings.Contains(out, secret) {
			t.Errorf("RedactBody leaked %q: %s", secret, out)
		}
	}
	for _, want := range []string{`"password":"REDACTED"`, `"login_token":"REDACTED"`, `"ok":1`} {
		if !strings.Contains(out, want) {
			t.Errorf("RedactBody result missing %q: %s", want, out)
		}
	}
}

func TestRedactBodyForm(t *testing.T) {
	body := []byte("loginID=me%40example.com&password=pw-SECRET&login_token=st2.SECRET&APIKey=k")
	out := RedactBody(body)
	for _, secret := range []string{"me%40example.com", "me@example.com", "pw-SECRET", "st2.SECRET"} {
		if strings.Contains(out, secret) {
			t.Errorf("RedactBody leaked %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "APIKey=k") {
		t.Errorf("RedactBody dropped a non-sensitive key: %s", out)
	}
	if !strings.Contains(out, "REDACTED") {
		t.Errorf("RedactBody missing the redaction marker: %s", out)
	}
}

func TestRedactBodyUnparseable(t *testing.T) {
	body := []byte("a=%zz")
	if got, want := RedactBody(body), fmt.Sprintf("<%d bytes>", len(body)); got != want {
		t.Errorf("RedactBody(%q) = %q, want %q", body, got, want)
	}
}

func TestRedactBodyEmpty(t *testing.T) {
	if got := RedactBody(nil); got != "" {
		t.Errorf("RedactBody(nil) = %q, want empty", got)
	}
	if got := RedactBody([]byte{}); got != "" {
		t.Errorf("RedactBody(empty) = %q, want empty", got)
	}
}
