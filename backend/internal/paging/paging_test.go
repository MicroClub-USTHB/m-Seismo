package paging

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	c := Cursor{CreatedAt: time.Date(2026, 10, 16, 12, 0, 0, 123456000, time.UTC), ID: uuid.New()}
	got, err := Decode(c.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(c.CreatedAt) || got.ID != c.ID {
		t.Fatalf("round trip = %+v, want %+v", got, c)
	}
	for _, bad := range []string{"!!", "bm9waXBl", "eHx5"} {
		if _, err := Decode(bad); err == nil {
			t.Errorf("Decode(%q) succeeded", bad)
		}
	}
	if c, err := Decode(""); c != nil || err != nil {
		t.Errorf("Decode(\"\") = %v, %v", c, err)
	}
}

func TestTrim(t *testing.T) {
	cursorOf := func(n int) Cursor { return Cursor{ID: uuid.UUID{byte(n)}} }
	if r := Trim([]int{1, 2}, 2, cursorOf); r.NextCursor != "" || len(r.Items) != 2 {
		t.Errorf("full last page = %+v", r)
	}
	r := Trim([]int{1, 2, 3}, 2, cursorOf)
	if len(r.Items) != 2 || r.NextCursor != cursorOf(2).Encode() {
		t.Errorf("trimmed page = %+v", r)
	}
}
