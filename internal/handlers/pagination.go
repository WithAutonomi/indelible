package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Cursor represents a pagination cursor for keyset-based pagination.
type Cursor struct {
	ID  int64  `json:"id"`
	Dir string `json:"dir"` // "next" or "prev"
}

// EncodeCursor serializes a cursor to a URL-safe string.
func EncodeCursor(c Cursor) string {
	b, _ := json.Marshal(c)
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeCursor deserializes a cursor from a URL-safe string.
func DecodeCursor(s string) (Cursor, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor")
	}
	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor")
	}
	return c, nil
}
