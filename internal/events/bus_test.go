package events

import "testing"

func TestBusFanOut(t *testing.T) {
	b := NewBus()
	a, cancelA := b.Subscribe()
	c, cancelC := b.Subscribe()
	defer cancelC()
	b.Publish("x", 1)
	if ev := <-a; ev.Type != "x" || ev.Data != 1 {
		t.Fatalf("a got %+v", ev)
	}
	if ev := <-c; ev.Type != "x" {
		t.Fatalf("c got %+v", ev)
	}
	cancelA()
	cancelA() // idempotent
	if _, ok := <-a; ok {
		t.Fatal("cancelled channel should be closed")
	}
	b.Publish("y", nil) // must not panic on closed subscriber
	if ev := <-c; ev.Type != "y" {
		t.Fatalf("c got %+v", ev)
	}
}

func TestBusDropsWhenFull(t *testing.T) {
	b := NewBus()
	_, cancel := b.Subscribe()
	defer cancel()
	for i := 0; i < 1000; i++ {
		b.Publish("spam", i) // never blocks
	}
}
