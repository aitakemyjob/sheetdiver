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
	if out, _, isErr := h.call("simulate", map[string]any{"project": "sim", "name": "bad", "netlist": "t\n.include /etc/passwd\n.op\n"}); !isErr || !strings.Contains(out, ".include isn't allowed") {
		t.Errorf("bad netlist: %s", out)
	}
	variants := []map[string]any{{"name": "1k"}, {"name": "100R", "params": map[string]any{"rval": "100"}}}
	if _, err := exec.LookPath("ngspice"); err != nil {
		h.must("simulate", map[string]any{"project": "sim", "name": "RC step", "netlist": rcNetlist, "variants": variants, "run": false}, "saved simulation")
		h.must("ngspice_status", map[string]any{}, "Simulations can't run")
		return
	}
	h.must("ngspice_status", map[string]any{}, "ngspice ")
	out := h.must("simulate", map[string]any{"project": "sim", "name": "RC step", "kind": "step response", "netlist": rcNetlist, "variants": variants},
		"RC step / 1k: done", "RC step / 100R: done", "vmax = ", "plot tran1 (Transient Analysis", "plot ac1 (AC Analysis")
	h.must("present_simulation", map[string]any{"project": "sim", "name": "RC step", "summary": "Smaller R charges faster.",
		"plots":  []map[string]any{{"title": "Step", "plot": "tran1", "signals": []string{"v(out)"}}, {"title": "Response", "plot": "ac1", "signals": []string{"v(out)"}, "xLog": true}},
		"checks": []map[string]any{{"measure": "vmax", "min": 3.0, "unit": "V"}}}, "presented", "1k / vmax", "FAIL", "100R / vmax", "pass")
	h.must("list_simulations", map[string]any{"project": "sim"}, "RC step [step response]", "1 pass, 1 fail", "presented true")
	h.must("get_simulation", map[string]any{"project": "sim", "name": "RC step"}, "variant 100R", "## Presentation")
	run, _ := strconv.Atoi(regexp.MustCompile(`run (\d+),`).FindStringSubmatch(out)[1])
	h.must("get_simulation_data", map[string]any{"run": run, "plot": "ac1", "signals": []string{"v(out)"}, "points": 5}, "v(out) (dB) against frequency (Hz)")
	h.must("show", map[string]any{"kind": "project", "project": "sim", "tab": "simulations", "highlight": "RC step"})
	h.must("delete_simulation", map[string]any{"project": "sim", "name": "RC step"}, "deleted")
	h.must("list_simulations", map[string]any{"project": "sim"}, "no simulations yet")
}
