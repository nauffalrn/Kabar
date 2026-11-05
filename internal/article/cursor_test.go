package article

import (
	"errors"
	"testing"
	"time"
)

func TestCursor_RoundTrip(t *testing.T) {
	want := Cursor{CreatedAt: time.Date(2026, 11, 3, 10, 0, 0, 123456000, time.UTC), ID: "0192f0c1-id"}

	got, err := DecodeCursor(want.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Fatalf("round trip lost data: got %+v, want %+v", got, want)
	}
}

func TestDecodeCursor_RejectsGarbage(t *testing.T) {
	for _, in := range []string{"!!!", "e30", "bm90LWpzb24"} { // invalid b64, "{}", "not-json"
		if _, err := DecodeCursor(in); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("DecodeCursor(%q) = %v, want ErrInvalidCursor", in, err)
		}
	}
}
