package blob

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

type getOnlyStore struct {
	body []byte
	err  error
	refs []Ref
}

func (s *getOnlyStore) Put(context.Context, []byte) (Ref, error) {
	return "", errors.New("unexpected put")
}
func (s *getOnlyStore) Get(ctx context.Context, ref Ref) ([]byte, error) {
	s.refs = append(s.refs, ref)
	if s.err != nil {
		return nil, s.err
	}
	return append([]byte(nil), s.body...), nil
}

func TestResolveLiteralReturnsBytes(t *testing.T) {
	got, err := Resolve(context.Background(), &getOnlyStore{}, "literal output")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got) != "literal output" {
		t.Fatalf("Resolve literal = %q", got)
	}
}

func TestResolveBlobRefReadsStore(t *testing.T) {
	ref, err := NewRef("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	store := &getOnlyStore{body: []byte("stored body")}
	got, err := Resolve(context.Background(), store, ref.String())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !bytes.Equal(got, store.body) {
		t.Fatalf("Resolve body = %q", got)
	}
	if len(store.refs) != 1 || store.refs[0] != ref {
		t.Fatalf("Get refs = %+v, want %s", store.refs, ref)
	}
}

func TestResolveBlobRefPropagatesStoreError(t *testing.T) {
	ref, err := NewRef("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("blob not found")
	_, err = Resolve(context.Background(), &getOnlyStore{err: want}, ref.String())
	if !errors.Is(err, want) {
		t.Fatalf("Resolve error = %v, want %v", err, want)
	}
}
