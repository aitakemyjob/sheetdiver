package core

// Which part a passive supports when nothing says, and which part it is
// drawn by: each decided by the first rule that applies, on facts about
// the passive alone, so each rule can be tested on its own.

// supportFacts is what the support rules decide on: what the passive is,
// the parts its nets reach, and what each is and drives.
type supportFacts struct {
	// kind is the passive's symbol kind (diode, tvs, capacitor…).
	kind string
	// reach lists the parts on the passive's nets, in order.
	reach []string
	// nets are the passive's nets.
	nets []string
	// kindOf is a part's kind (ic, connector, module…).
	kindOf func(id string) string
	// drives reports whether a part drives a net.
	drives func(id, net string) bool
}

// supportRule guesses the part a passive supports, if it can.
type supportRule func(f supportFacts) (string, bool)

// supportRules are tried in this order.
var supportRules = []supportRule{protectionByConnector, theOnlyIC, theOnlyPart, theOneItFeeds}

// guessSupport is the part a passive supports when nothing says: the
// first rule's answer, else "".
func guessSupport(f supportFacts) string {
	for _, rule := range supportRules {
		if id, ok := rule(f); ok {
			return id
		}
	}
	return ""
}

// ofKind lists the parts of a kind among those reached.
func (f supportFacts) ofKind(kind string) []string {
	var out []string
	for _, m := range f.reach {
		if f.kindOf(m) == kind {
			out = append(out, m)
		}
	}
	return out
}

// protectionByConnector: a protection diode of any kind (TVS, Zener,
// Schottky) goes by the one connector it protects.
func protectionByConnector(f supportFacts) (string, bool) {
	switch f.kind {
	case "diode", "tvs", "zener", "schottky":
		if c := f.ofKind("connector"); len(c) == 1 {
			return c[0], true
		}
	}
	return "", false
}

// theOnlyIC: the one IC on its nets, over connectors and modules.
func theOnlyIC(f supportFacts) (string, bool) {
	if ics := f.ofKind("ic"); len(ics) == 1 {
		return ics[0], true
	}
	return "", false
}

// theOnlyPart: the one part on its nets, when no IC is.
func theOnlyPart(f supportFacts) (string, bool) {
	if len(f.ofKind("ic")) == 0 && len(f.reach) == 1 {
		return f.reach[0], true
	}
	return "", false
}

// theOneItFeeds: of several ICs (else parts), the one that doesn't drive
// any of its nets: a bulk capacitor between an eFuse and a regulator
// goes by the regulator's input.
func theOneItFeeds(f supportFacts) (string, bool) {
	pool := f.ofKind("ic")
	if len(pool) == 0 {
		pool = f.reach
	}
	var uses []string
	for _, m := range pool {
		drives := false
		for _, net := range f.nets {
			drives = drives || f.drives(m, net)
		}
		if !drives {
			uses = append(uses, m)
		}
	}
	if len(uses) == 1 {
		return uses[0], true
	}
	return "", false
}

// placedBy reports whether a passive on a net is drawn by the part it
// supports: when that part is on the net, reaches it through a passive
// that carries it (the output capacitor after the inductor), or is the
// source of the rail.
func placedBy(serves string, onNet, through []string, sourceOfNet bool) bool {
	return serves != "" && (contains(onNet, serves) || contains(through, serves) || sourceOfNet)
}
