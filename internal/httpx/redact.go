package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// redactedValue replaces every sensitive value in logs.
const redactedValue = "REDACTED"

// sensitiveKeys is the set of query keys, header names, JSON keys and form
// keys whose values never reach a log line. Matching is case-insensitive and
// the whole value is replaced, including object and array values.
var sensitiveKeys = map[string]struct{}{
	"login_token":         {},
	"loginid":             {},
	"password":            {},
	"uid":                 {},
	"uidsignature":        {},
	"signature":           {},
	"signaturetimestamp":  {},
	"usergigyauid":        {},
	"ptb-gigya-user":      {},
	"cookie":              {},
	"set-cookie":          {},
	"gmid":                {},
	"email":               {},
	"firstname":           {},
	"lastname":            {},
	"profile":             {},
	"data":                {},
	"phones":              {},
	"phone":               {},
	"name":                {},
	"nif":                 {},
	"street":              {},
	"number":              {},
	"floor":               {},
	"door":                {},
	"details":             {},
	"placeid":             {},
	"fulladdress":         {},
	"autocompletecontent": {},
	"currentfulladdress":  {},
	"currentstreet":       {},
	"latitude":            {},
	"longitude":           {},
	"location":            {},
	"input":               {},
	"externalid":          {},
	"referencesac":        {},
	"postalcode":          {},
	"riskcontext":         {},
	"sessioninfo":         {},
	// The Gigya login answers with the account email, name and provider ids
	// outside the profile, because the CLI asks it to include them.
	"emails":           {},
	"loginids":         {},
	"userinfo":         {},
	"identities":       {},
	"nickname":         {},
	"uidsig":           {},
	"loginprovideruid": {},
	"provideruid":      {},
	// The town and street codes of the delivery address. The address and
	// agency objects are dropped whole, so their partial history copies go too.
	"address":          {},
	"agency":           {},
	"locality":         {},
	"localitycode":     {},
	"municipality":     {},
	"municipalitycode": {},
	"streetcode":       {},
}

// isSensitiveKey reports whether a query, header, JSON or form key holds a
// value that must never be logged.
func isSensitiveKey(key string) bool {
	_, ok := sensitiveKeys[strings.ToLower(strings.TrimSpace(key))]
	return ok
}

// RedactURL returns the URL with every sensitive query value and any user
// information replaced by "REDACTED". It is safe to put in a log line.
func RedactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	if clone.User != nil {
		clone.User = url.User(redactedValue)
	}
	if clone.RawQuery != "" {
		query := clone.Query()
		for key := range query {
			if isSensitiveKey(key) {
				query.Set(key, redactedValue)
			}
		}
		clone.RawQuery = query.Encode()
	}
	return clone.String()
}

// RedactHeader returns the header values as a map with every sensitive header
// replaced by "REDACTED". Values of non-sensitive headers with several
// entries are joined with ", ".
func RedactHeader(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for key, values := range h {
		if isSensitiveKey(key) {
			out[key] = redactedValue
			continue
		}
		out[key] = strings.Join(values, ", ")
	}
	return out
}

// RedactBody returns a log-safe rendering of a request or response body. JSON
// bodies keep their structure with sensitive values replaced; form bodies are
// re-encoded the same way. Anything else is reduced to "<N bytes>".
func RedactBody(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	var decoded any
	if err := json.Unmarshal(b, &decoded); err == nil {
		if encoded, err := json.Marshal(redactJSONValue(decoded)); err == nil {
			return string(encoded)
		}
	}

	if values, err := url.ParseQuery(string(b)); err == nil && len(values) > 0 {
		for key := range values {
			if isSensitiveKey(key) {
				values.Set(key, redactedValue)
			}
		}
		return values.Encode()
	}

	return fmt.Sprintf("<%d bytes>", len(b))
}

// redactJSONValue walks a decoded JSON value and replaces every sensitive key
// with "REDACTED", including object and array values.
func redactJSONValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			if isSensitiveKey(key) {
				out[key] = redactedValue
				continue
			}
			out[key] = redactJSONValue(value)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, value := range typed {
			out[i] = redactJSONValue(value)
		}
		return out
	default:
		return v
	}
}
