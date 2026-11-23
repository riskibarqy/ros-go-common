package requestcontext_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/riskibarqy/ros-go-common/requestcontext"
)

func TestRequestIDs(t *testing.T) {
	for _, value := range []string{"abc-_.123", strings.Repeat("a", 128)} {
		id, err := requestcontext.ParseRequestID(value)
		if err != nil || id.String() != value {
			t.Fatal(id, err)
		}
		data, err := json.Marshal(id)
		if err != nil || string(data) != `"`+value+`"` {
			t.Fatal(string(data), err)
		}
	}
	for _, value := range []string{"", strings.Repeat("a", 129), "a b", "id\r\nHeader: value", "é", "💡", "a/b"} {
		if _, err := requestcontext.ParseRequestID(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	seen := map[string]bool{}
	for range 100 {
		id := requestcontext.GenerateRequestID()
		value := id.String()
		if len(value) != 36 || value[14] != '4' || !strings.ContainsRune("89ab", rune(value[19])) || seen[value] {
			t.Fatal(value)
		}
		seen[value] = true
		if _, err := requestcontext.ParseRequestID(value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := requestcontext.New(requestcontext.RequestID{}); err == nil {
		t.Fatal("accepted zero ID")
	}
	if _, err := json.Marshal(requestcontext.RequestID{}); err == nil {
		t.Fatal("serialized zero ID")
	}
}
func TestRequestTiming(t *testing.T) {
	request, err := requestcontext.New(requestcontext.GenerateRequestID())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := request.Remaining(); ok {
		t.Fatal("invented deadline")
	}
	timed, err := request.WithTimeout(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	deadline, ok := timed.Deadline()
	if !ok || !deadline.Equal(request.StartedAt().Add(time.Minute)) {
		t.Fatal(deadline)
	}
	if _, ok := request.Deadline(); ok {
		t.Fatal("mutated original")
	}
	if remaining, ok := timed.Remaining(); !ok || remaining <= 0 || remaining > time.Minute {
		t.Fatal(remaining)
	}
	expired := request.WithDeadline(time.Now().Add(-time.Second))
	if left, ok := expired.Remaining(); !ok || left != 0 {
		t.Fatal(left)
	}
	if _, err := request.WithTimeout(-1); err == nil {
		t.Fatal("accepted negative timeout")
	}
	ctx := requestcontext.WithContext(context.Background(), timed)
	got, ok := requestcontext.FromContext(ctx)
	if !ok || got.RequestID() != timed.RequestID() || got.StartedAt() != timed.StartedAt() {
		t.Fatal(got)
	}
}
