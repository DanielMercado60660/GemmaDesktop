package harness

import "testing"

func TestUpstreamBindingsReconstructAndRejectTamperedPacketProvenance(t *testing.T) {
	m, spec, base := developmentManager(t, nil)
	spec.Mode = "upstream"
	x, err := m.Start(spec, base, Author{})
	if err != nil {
		t.Fatal(err)
	}
	var inv Packet
	for _, stage := range []string{"proposal_inv", "proposal_dom", "compose"} {
		p, err := m.MakePacket(x, "development", stage, x.Spec.BaseRevisionID, "public evidence", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.recordPacket(&x, p); err != nil {
			t.Fatal(err)
		}
		p, err = m.Submit(PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: Author{Kind: "codex", Identity: "proposal-author"}, Content: &base, Notes: "KEEP_BASE"})
		if err != nil {
			t.Fatal(err)
		}
		if stage == "proposal_inv" {
			inv = p
		}
	}
	bindings, err := m.UpstreamRevisionBindings(x)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 4 || bindings[0].Label != "base" || bindings[0].RevisionID != x.Spec.BaseRevisionID {
		t.Fatal("baseline/stage reconstruction lost", bindings)
	}
	for _, b := range bindings[1:] {
		if b.RevisionID != inv.RevisionID || b.PacketID == "" || b.RequestHash == "" || b.RevisionID == x.Spec.BaseRevisionID {
			t.Fatal("equal-content stage provenance lost", b)
		}
	}
	wrongParent, err := m.CreateRevision(x.Spec, inv.RevisionID, base, Author{Kind: "codex", Identity: "proposal-author"})
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Packet){
		"request context":   func(p *Packet) { p.Mission = "changed mission" },
		"response identity": func(p *Packet) { p.Response.ID = "changed-id" },
		"response content":  func(p *Packet) { p.Response.Content.Instructions = "changed content" },
		"base identity":     func(p *Packet) { p.BaseRevisionID = inv.RevisionID },
		"revision parent":   func(p *Packet) { p.RevisionID = wrongParent.ID },
		"task identity":     func(p *Packet) { p.TaskID = "unrelated-task" },
	} {
		t.Run(name, func(t *testing.T) {
			var p Packet
			if err := m.get("packet", inv.ID, &p); err != nil {
				t.Fatal(err)
			}
			mutate(&p)
			if err := m.put("packet", p); err != nil {
				t.Fatal(err)
			}
			if _, err := m.UpstreamRevisionBindings(x); err == nil {
				t.Fatal("tampered packet accepted")
			}
			if err := m.put("packet", inv); err != nil {
				t.Fatal(err)
			}
		})
	}
	x.Spec.ModelDigest = "changed"
	if _, err := m.UpstreamRevisionBindings(x); err == nil {
		t.Fatal("tampered frozen spec accepted")
	}
}
