package externalpush

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
)

func responseStatus(body []byte) (json.Number, error) {
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.UseNumber()
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return "", errors.New("response is not a complete JSON object")
	}
	var status json.Number
	seen := false
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return "", errors.New("response is not a complete JSON object")
		}
		var value interface{}
		if err := d.Decode(&value); err != nil {
			return "", errors.New("response is not a complete JSON object")
		}
		if key == "status" {
			if seen {
				return "", errors.New("response contains duplicate business status keys")
			}
			seen = true
			var ok bool
			status, ok = value.(json.Number)
			if !ok {
				return "", errors.New("business status is not numeric")
			}
		}
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return "", errors.New("response is not a complete JSON object")
	}
	if err := d.Decode(new(interface{})); err != io.EOF {
		return "", errors.New("trailing response data")
	}
	if !seen {
		return "", errors.New("business status is not numeric")
	}
	return status, nil
}

// Compare a JSON decimal exactly, without rounding or allocating enormous
// big integers from an untrusted exponent. The decoder already checked grammar.
func statusIs200(number json.Number) bool {
	raw := string(number)
	if strings.HasPrefix(raw, "-") {
		return false
	}
	exponent := 0
	if i := strings.IndexAny(raw, "eE"); i >= 0 {
		var err error
		exponent, err = strconv.Atoi(raw[i+1:])
		if err != nil || exponent > 65536 || exponent < -65536 {
			return false
		}
		raw = raw[:i]
	}
	if i := strings.IndexByte(raw, '.'); i >= 0 {
		exponent -= len(raw) - i - 1
		raw = raw[:i] + raw[i+1:]
	}
	raw = strings.TrimLeft(raw, "0")
	trimmed := strings.TrimRight(raw, "0")
	exponent += len(raw) - len(trimmed)
	return trimmed == "2" && exponent == 2
}
