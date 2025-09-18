package streaming

import "testing"

func TestRingReplaySince(t *testing.T) {
	r := newRing(3)
	// Push 4 events, which will overwrite the first
	for i := 0; i < 4; i++ {
		r.push(Event{Seq: uint64(i + 1)})
	}
	// Expect ring holds seq 2,3,4
	evs := r.since(0)
	if len(evs) != 3 || evs[0].Seq != 2 || evs[2].Seq != 4 {
		t.Fatalf("unexpected ring contents: %+v", evs)
	}
	// Replay since 2 -> expect 3,4
	evs = r.since(2)
	if len(evs) != 2 || evs[0].Seq != 3 || evs[1].Seq != 4 {
		t.Fatalf("unexpected replay since 2: %+v", evs)
	}
}

func TestManagerReplayIntegration(t *testing.T) {
	m := Get()
	wf := "wf-test"
	m.capacity = 5
	for i := 0; i < 5; i++ {
		m.Publish(wf, Event{WorkflowID: wf})
	}
	// Next publish increments seq; replay since 3 should return seq 4..6 depending on internal assignment
	evs := m.ReplaySince(wf, 3)
	for _, e := range evs {
		if e.Seq <= 3 {
			t.Fatalf("replay returned stale seq: %d", e.Seq)
		}
	}
}
