package keychain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestSafe(t *testing.T) {
	for s, want := range map[string]bool{
		"sk-ant-api03-AbC_d.e": true,
		"":                     false,
		`a"b`:                  false,
		"a b":                  false,
		"a\nb":                 false,
		"a\\b":                 false,
	} {
		if got := safe(s); got != want {
			t.Errorf("safe(%q) = %v, want %v", s, got, want)
		}
	}
}

// TestRoundTrip writes, reads and deletes a throwaway item in the real
// login keychain. It only runs when asked, since it touches the keychain.
func TestRoundTrip(t *testing.T) {
	if os.Getenv("BREWCTL_KEYCHAIN_TEST") == "" {
		t.Skip("set BREWCTL_KEYCHAIN_TEST=1 to use the login keychain")
	}
	ctx := context.Background()
	it := Item{Service: "dev.vimukthid.brewctl.test", Account: fmt.Sprintf("t%d", time.Now().UnixNano())}
	t.Cleanup(func() { _ = it.Delete(ctx) })

	if _, err := it.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get before Set: %v, want ErrNotFound", err)
	}
	for _, v := range []string{"first-value", "second_value.2"} {
		if err := it.Set(ctx, v); err != nil {
			t.Fatal(err)
		}
		got, err := it.Get(ctx)
		if err != nil || got != v {
			t.Fatalf("Get = %q, %v; want %q", got, err, v)
		}
	}
	if err := it.Set(ctx, `bad"value`); err == nil {
		t.Fatal("Set accepted a quote")
	}
	if err := it.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := it.Delete(ctx); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if _, err := it.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: %v", err)
	}
}
