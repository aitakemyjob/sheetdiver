package library

import (
	"context"
	"errors"
	"testing"
)

func TestGroupsAndAssignments(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	ctx := context.Background()
	p, _ := s.CreateProject(ctx, "Sensor", "")
	part, _ := s.Resolve(ctx, "XYZ120")
	u1, _ := s.AddProjectPart(ctx, ProjectPart{ProjectID: p.ID, PartID: part.Part.ID, Designator: "U1"})
	caps, _ := s.AddProjectPart(ctx, ProjectPart{ProjectID: p.ID, PartID: part.Part.ID, Designator: "C1,C2"})
	usb, _ := s.SetBlock(ctx, Block{ProjectID: p.ID, Name: "USB-C"})

	if _, err := s.SetGroup(ctx, Group{ProjectID: p.ID, Name: " "}); err == nil {
		t.Error("a group needs a name")
	}
	if _, err := s.SetGroup(ctx, Group{ProjectID: p.ID, Name: "Power", Description: "12 V in, 3V3 out"}); err != nil {
		t.Fatal(err)
	}
	// The same name (any case) updates it.
	if _, err := s.SetGroup(ctx, Group{ProjectID: p.ID, Name: "power", Description: "Buck", Notes: "keep the loop small", Source: SourceAgent}); err != nil {
		t.Fatal(err)
	}
	groups, _ := s.Groups(ctx, p.ID)
	if len(groups) != 1 || groups[0].Name != "Power" || groups[0].Description != "Buck" || groups[0].Notes != "keep the loop small" || groups[0].Source != SourceAgent {
		t.Fatalf("groups = %+v", groups)
	}

	// Assigning to a group that doesn't exist creates it.
	must := func(a Assignment) {
		t.Helper()
		if err := s.Assign(ctx, p.ID, a); err != nil {
			t.Fatal(err)
		}
	}
	must(Assignment{Node: PartNode(u1.ID), Group: "Power"})
	must(Assignment{Node: RefNode(caps.ID, "C1"), Group: "MCU", Supports: PartNode(u1.ID)})
	must(Assignment{Node: RefNode(caps.ID, "C2"), Supports: PartNode(u1.ID)})
	must(Assignment{Node: BlockNode(usb.ID), Group: "Power"})
	if err := s.Assign(ctx, p.ID, Assignment{Node: PartNode(u1.ID), Supports: PartNode(u1.ID)}); err == nil {
		t.Error("a part can't support itself")
	}
	if groups, _ = s.Groups(ctx, p.ID); len(groups) != 2 || groups[0].Name != "MCU" {
		t.Errorf("groups = %+v", groups)
	}
	as, _ := s.Assignments(ctx, p.ID)
	if len(as) != 4 {
		t.Fatalf("assignments = %+v", as)
	}

	// Removing a group leaves its members ungrouped; what they support stays.
	if err := s.DeleteGroup(ctx, p.ID, "mcu"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(ctx, p.ID, "mcu"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting again: %v", err)
	}
	as, _ = s.Assignments(ctx, p.ID)
	for _, a := range as {
		if a.Node == RefNode(caps.ID, "C1") && (a.Group != "" || a.Supports != PartNode(u1.ID)) {
			t.Errorf("C1 = %+v", a)
		}
	}

	// Clearing both forgets a node.
	must(Assignment{Node: BlockNode(usb.ID)})
	// Removing a part forgets it, and what said it supports it.
	if err := s.RemoveProjectPart(ctx, p.ID, u1.ID); err != nil {
		t.Fatal(err)
	}
	if as, _ = s.Assignments(ctx, p.ID); len(as) != 0 {
		t.Errorf("after removing U1 and USB-C's: %+v", as)
	}

	// A removed block is forgotten too.
	must(Assignment{Node: BlockNode(usb.ID), Group: "Power"})
	must(Assignment{Node: RefNode(caps.ID, "C1"), Supports: BlockNode(usb.ID)})
	if err := s.DeleteBlock(ctx, p.ID, "USB-C"); err != nil {
		t.Fatal(err)
	}
	if as, _ = s.Assignments(ctx, p.ID); len(as) != 0 {
		t.Errorf("after removing USB-C: %+v", as)
	}
}
