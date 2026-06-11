package engine

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// A comment thread on any record: the customer sees the conversation but not
// the operator's internal notes; staff (who can update) see everything; the
// thread survives replay.
func TestComments(t *testing.T) {
	src := `
entity Ticket:
    title: string required
    customer: ref[core.User]

roles:
    Agent
    Customer

permissions:
    Agent:
        full [Ticket]
    Customer:
        read Ticket where customer = $me
        deny [delete *, update Ticket.*]
`
	model, errs := dsl.Compile(map[string]string{"t.kal": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	agent := eventstore.Actor{Type: eventstore.ActorHuman, ID: "op", Role: "Agent"}
	ivan := eventstore.Actor{Type: eventstore.ActorHuman, ID: "ivan", Role: "Customer"}

	tk, _ := e.Create(ctx, agent, "Ticket", map[string]any{"title": "broken", "customer": "ivan"}, basis, "")

	// customer posts; operator replies publicly; operator adds an internal note
	if _, err := e.Comment(ctx, ivan, "Ticket", tk.ID, "It does not work", false, basis); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Comment(ctx, agent, "Ticket", tk.ID, "Looking into it", false, basis); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Comment(ctx, agent, "Ticket", tk.ID, "probably their cache, escalate if not", true, basis); err != nil {
		t.Fatal(err)
	}

	// customer may NOT post an internal note (no write rights on the host)
	if _, err := e.Comment(ctx, ivan, "Ticket", tk.ID, "sneaky", true, basis); err == nil {
		t.Fatal("customer must not post internal comments")
	}

	// the customer sees 2 public, the agent sees all 3
	cust, _ := e.CommentsOf(ivan, "Ticket", tk.ID)
	if len(cust) != 2 {
		t.Fatalf("customer must see 2 public comments, got %d", len(cust))
	}
	for _, c := range cust {
		if c.Internal {
			t.Fatal("customer must never see an internal note")
		}
	}
	staff, _ := e.CommentsOf(agent, "Ticket", tk.ID)
	if len(staff) != 3 {
		t.Fatalf("agent must see all 3 comments, got %d", len(staff))
	}

	// a customer cannot read a thread on someone else's ticket (invisible host)
	bob := eventstore.Actor{Type: eventstore.ActorHuman, ID: "bob", Role: "Customer"}
	if _, err := e.CommentsOf(bob, "Ticket", tk.ID); err == nil {
		t.Fatal("bob must not read ivan's ticket thread")
	}
}
