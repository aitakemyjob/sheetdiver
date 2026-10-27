package mcpserver

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// rcNetlist is an RC low-pass with a step and its frequency response.
const rcNetlist = `RC low-pass
.param rval=1k
V1 in 0 PULSE(0 3.3 0 1u 1u 1m 2m) AC 1
R1 in out {rval}
C1 out 0 1u
.tran 10u 3m
.ac dec 10 10 100k
.meas tran vmax MAX v(out)
.end
`

func TestSimulationTools(t *testing.T) {
	h := newHarness(t)
	h.must("create_project", map[string]any{"name": "sim"})
	// A netlist that reads files never runs.
	if out, _, isErr := h.call("simulate", map[string]any{"project": "sim", "test": "bad", "variation": "a", "netlist": "t\n.include /etc/passwd\n.op\n"}); !isErr || !strings.Contains(out, ".include isn't allowed") {
		t.Errorf("bad netlist: %s", out)
	}
	conditions := []map[string]any{{"name": "room"}, {"name": "hot", "temp": 85}}
	revB := strings.Replace(rcNetlist, ".param rval=1k", ".param rval=100", 1)
	if _, err := exec.LookPath("ngspice"); err != nil {
		h.must("simulate", map[string]any{"project": "sim", "test": "RC step", "variation": "Rev A", "netlist": rcNetlist, "conditions": conditions, "run": false}, "saved variation \"Rev A\" of test \"RC step\"")
		h.must("ngspice_status", map[string]any{}, "Simulations can't run")
		return
	}
	h.must("ngspice_status", map[string]any{}, "ngspice ")
	out := h.must("simulate", map[string]any{"project": "sim", "test": "RC step", "kind": "step response", "variation": "Rev A", "change": "first draft", "netlist": rcNetlist, "conditions": conditions},
		"RC step / Rev A / room: done", "RC step / Rev A / hot: done", "vmax = ", "plot tran1 (Transient Analysis", "plot ac1 (AC Analysis")
	// The schematic changes: a second variation of the same test, the first kept.
	h.must("simulate", map[string]any{"project": "sim", "test": "RC step", "variation": "Rev B", "change": "R1 down to 100 Ω", "netlist": revB, "conditions": conditions}, "RC step / Rev B / room: done")
	h.must("present_simulation", map[string]any{"project": "sim", "test": "RC step", "summary": "Rev B charges fully.",
		"plots":  []map[string]any{{"title": "Step", "plot": "tran1", "signals": []string{"v(out)"}, "variations": []string{"Rev A", "Rev B"}, "conditions": []string{"room"}}, {"title": "Response", "plot": "ac1", "signals": []string{"v(out)"}}},
		"checks": []map[string]any{{"measure": "vmax", "min": 3.0, "unit": "V"}},
		"notes":  []map[string]any{{"variation": "Rev A", "text": "too slow"}}},
		"presented", "Rev A / room / vmax", "FAIL", "Rev B / hot / vmax", "pass")
	h.must("list_simulations", map[string]any{"project": "sim"}, "RC step [step response]", "2 variations, latest \"Rev B\"", "2 pass, 0 fail", "written up true")
	h.must("get_simulation", map[string]any{"project": "sim", "test": "RC step"}, "## Variation 1: Rev A", "## Variation 2: Rev B", "R1 down to 100 Ω", "## Write-up")
	run, _ := strconv.Atoi(regexp.MustCompile(`run (\d+),`).FindStringSubmatch(out)[1])
	h.must("get_simulation_data", map[string]any{"run": run, "plot": "ac1", "signals": []string{"v(out)"}, "points": 5}, "(Rev A, room)", "v(out) (dB) against frequency (Hz)")
	h.must("show", map[string]any{"kind": "project", "project": "sim", "tab": "simulations", "highlight": "RC step"})
	h.must("delete_variation", map[string]any{"project": "sim", "test": "RC step", "variation": "Rev A"}, "deleted variation Rev A")
	h.must("list_simulations", map[string]any{"project": "sim"}, "1 variations, latest \"Rev B\"")
	h.must("delete_simulation", map[string]any{"project": "sim", "test": "RC step"}, "deleted")
	h.must("list_simulations", map[string]any{"project": "sim"}, "no tests yet")
}
