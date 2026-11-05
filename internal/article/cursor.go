package article

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// Cursor is the keyset position (created_at, id) of the last item seen.
type Cursor struct {
	CreatedAt time.Time `json:"t"`
	ID        string    `json:"id"`
}

func CursorOf(a Article) Cursor { return Cursor{CreatedAt: a.CreatedAt, ID: a.ID} }

// Encode returns an opaque token so clients cannot depend on its format.
func (c Cursor) Encode() string {
	b, _ := json.Marshal(c) // cannot fail for this struct
	return base64.RawURLEncoding.EncodeToString(b)
}

func DecodeCursor(s string) (*Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil || c.ID == "" || c.CreatedAt.IsZero() {
		return nil, ErrInvalidCursor
	}
	return &c, nil
}
