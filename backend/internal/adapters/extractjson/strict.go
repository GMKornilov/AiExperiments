// Package extractjson validates untrusted JSON at the infrastructure boundary.
package extractjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

var ErrInvalid = errors.New("invalid JSON response")

// Decode rejects unknown fields, duplicate object keys and trailing values.
func Decode(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	if err := value(d); err != nil {
		return ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return ErrInvalid
	}
	return nil
}
func value(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		keys := map[string]bool{}
		for d.More() {
			t, e := d.Token()
			if e != nil {
				return e
			}
			key, ok := t.(string)
			if !ok || keys[key] {
				return ErrInvalid
			}
			keys[key] = true
			if e = value(d); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if err := value(d); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	_, err = d.Token()
	return err
}
