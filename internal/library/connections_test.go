package library

import (
	"context"
	"errors"
	"testing"
)

func TestConnections(t *testing.T) {
	s := openTemp(t)
	ds := seed(t, s)
	ctx := context.Background()
	p, _ := s.CreateProject(ctx, "Sensor", "")
	part, _ := s.Resolve(ctx, "XYZ120")
	u1, err := s.AddProjectPart(ctx, ProjectPart{ProjectID: p.ID, PartID: part.Part.ID, Designator: "U1"})
	if err != nil {
		t.Fatal(err)
	}
	if bom, _ := s.ProjectParts(ctx, p.ID); bom[0].DatasheetID != ds || !bom[0].HasDatasheet {
		t.Errorf("an entry knows its datasheet (for its pins): %+v", bom[0])
	}
	bat, err := s.SetBlock(ctx, Block{ProjectID: p.ID, Name: "Battery"})
	if err != nil {
		t.Fatal(err)
	}

	// Connecting to a new net creates it, as a signal net.
	if _, err := s.Connect(ctx, p.ID, Connection{Net: "EN", EntryID: u1.ID, Pin: "3"}); err != nil {
		t.Fatal(err)
	}
	// A pin is on one net: connecting it again moves it.
	if _, err := s.Connect(ctx, p.ID, Connection{Net: "PG", EntryID: u1.ID, Pin: "3", Source: SourceAgent}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Connect(ctx, p.ID, Connection{Net: "VBAT", BlockID: bat.ID}); err != nil {
		t.Fatal(err)
	}
	// A pinless connection to the same net isn't duplicated.
	if _, err := s.Connect(ctx, p.ID, Connection{Net: "vbat", BlockID: bat.ID, Notes: "2S"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Connect(ctx, p.ID, Connection{Net: "X", EntryID: u1.ID, BlockID: bat.ID}); err == nil {
		t.Error("a connection is for an entry or a block, not both")
	}
	cs, _ := s.Connections(ctx, p.ID)
	if len(cs) != 2 || cs[0].Net != "PG" || cs[0].Pin != "3" || cs[0].Source != SourceAgent || cs[1].Net != "VBAT" || cs[1].BlockID != bat.ID || cs[1].Notes != "2S" {
		t.Fatalf("connections = %+v", cs)
	}
	if _, err := s.SetNet(ctx, Net{ProjectID: p.ID, Name: "vbat", Kind: NetPower}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetNet(ctx, Net{ProjectID: p.ID, Name: "Z", Kind: "analog"}); err == nil {
		t.Error("unknown kinds are refused")
	}
	nets, _ := s.Nets(ctx, p.ID)
	if len(nets) != 3 || nets[2].Name != "VBAT" || nets[2].Kind != NetPower {
		t.Errorf("nets = %+v", nets)
	}

	if err := s.Disconnect(ctx, p.ID, Connection{Net: "pg", EntryID: u1.ID, Pin: "3"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Disconnect(ctx, p.ID, Connection{Net: "PG", EntryID: u1.ID, Pin: "3"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("disconnecting twice: %v", err)
	}

	// Positions are kept until the node goes.
	for _, pos := range []Position{{Node: PartNode(u1.ID), X: 10, Y: 20}, {Node: BlockNode(bat.ID), X: 0, Y: 0}} {
		if err := s.SetPosition(ctx, p.ID, pos); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteBlock(ctx, p.ID, "battery"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveProjectPart(ctx, p.ID, u1.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.Layout(ctx, p.ID); len(l) != 0 {
		t.Errorf("layout of removed nodes = %+v", l)
	}
	if cs, _ := s.Connections(ctx, p.ID); len(cs) != 0 {
		t.Errorf("connections of removed nodes = %+v", cs)
	}
	if err := s.DeleteNet(ctx, p.ID, "VBAT"); err != nil {
		t.Fatal(err)
	}
	// Deleting the project takes the rest.
	if err := s.DeleteProject(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM project_nets) + (SELECT COUNT(*) FROM project_blocks) + (SELECT COUNT(*) FROM project_layout)`).Scan(&n)
	if n != 0 {
		t.Errorf("%d rows left after deleting the project", n)
	}
}
