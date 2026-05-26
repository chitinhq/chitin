package blob

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type mapStore struct {
	bodyByRef map[string][]byte
	err       error
}

func (s mapStore) Put(context.Context, []byte) (Ref, error) {
	return Ref{}, nil
}

func (s mapStore) Get(_ context.Context, ref Ref) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	body, ok := s.bodyByRef[ref.String()]
	if !ok {
		return nil, errors.New("missing blob")
	}
	return body, nil
}

func TestResolve(t *testing.T) {
	body := []byte("stored body")
	ref, err := RefFromHash(strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	store := mapStore{bodyByRef: map[string][]byte{ref.String(): body}}

	got, err := Resolve(context.Background(), store, "literal")
	if err != nil {
		t.Fatalf("Resolve literal: %v", err)
	}
	if string(got) != "literal" {
		t.Fatalf("Resolve literal = %q", got)
	}

	got, err = Resolve(context.Background(), store, ref.String())
	if err != nil {
		t.Fatalf("Resolve ref: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("Resolve ref body = %q, want %q", got, body)
	}

	_, err = Resolve(context.Background(), store, "blob://sha256/"+strings.Repeat("c", 64))
	if err == nil || !strings.Contains(err.Error(), "missing blob") {
		t.Fatalf("Resolve missing err = %v, want underlying missing blob error", err)
	}
}

func TestResolveTextReplacesBlobTokens(t *testing.T) {
	ref, err := RefFromHash(strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	store := mapStore{bodyByRef: map[string][]byte{ref.String(): []byte("resolved")}}
	got, err := ResolveText(context.Background(), store, "prefix "+ref.String()+" suffix")
	if err != nil {
		t.Fatalf("ResolveText: %v", err)
	}
	if got != "prefix resolved suffix" {
		t.Fatalf("ResolveText = %q", got)
	}
}
