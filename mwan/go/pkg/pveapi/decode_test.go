package pveapi

import "testing"

func TestDecodeAgentB64(t *testing.T) {
	t.Parallel()
	out, err := decodeAgentB64("")
	if err != nil || out != "" {
		t.Fatalf("empty: %q %v", out, err)
	}
	out, err = decodeAgentB64("aGVsbG8=")
	if err != nil || out != "hello" {
		t.Fatalf("hello: %q %v", out, err)
	}
}
