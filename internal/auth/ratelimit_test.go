package auth

import (
	"context"
	"testing"
	"time"
)

func TestKeyedSemaphoreAcquireWaits(t *testing.T) {
	k := NewKeyedSemaphore(2)
	if !k.TryAcquire("a") || !k.TryAcquire("a") || k.TryAcquire("a") {
		t.Fatal("limit 2 per key")
	}
	if !k.TryAcquire("b") {
		t.Fatal("keys are independent")
	}

	// sem slot: desiste quando o contexto vence
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if k.Acquire(ctx, "a") {
		t.Fatal("acquired over the limit")
	}

	// com espera: entra assim que alguém libera
	got := make(chan bool, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		got <- k.Acquire(ctx, "a")
	}()
	select {
	case <-got:
		t.Fatal("acquired before a release")
	case <-time.After(50 * time.Millisecond):
	}
	k.Release("b") // liberar outra chave acorda, mas não dá o slot
	select {
	case <-got:
		t.Fatal("acquired on another key's release")
	case <-time.After(50 * time.Millisecond):
	}
	k.Release("a")
	select {
	case ok := <-got:
		if !ok {
			t.Fatal("waiter gave up")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter not woken by release")
	}
	if k.TryAcquire("a") {
		t.Fatal("waiter must hold the freed slot")
	}
}
