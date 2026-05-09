package copilot

import (
	"context"
	"testing"
)

func TestClient_Start_NilClient(t *testing.T) {
	c := &Client{}
	if err := c.Start(context.Background()); err == nil {
		t.Error("expected error for nil sdkClient")
	}
}

func TestClient_Close_NilClient(t *testing.T) {
	c := &Client{}
	if err := c.Close(); err != nil {
		t.Errorf("expected nil error for nil sdkClient Close, got: %v", err)
	}
}

func TestClient_SDKClient_NilClient(t *testing.T) {
	c := &Client{}
	if sc := c.SDKClient(); sc != nil {
		t.Errorf("expected nil SDKClient, got: %v", sc)
	}
}