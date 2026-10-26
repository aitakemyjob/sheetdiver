# Loads a board with KiCad's own code and prints, as JSON, each part's
# place and its pads' nets and positions, and the DRC's courtyard
# overlaps: what the board tests check against.
import json, sys, tempfile, os
import pcbnew

board = pcbnew.LoadBoard(sys.argv[1])
out = {"footprints": [], "drc": []}
for fp in board.GetFootprints():
    pos = fp.GetPosition()
    pads = []
    for pad in fp.Pads():
        p = pad.GetPosition()
        pads.append({"number": pad.GetNumber(), "net": pad.GetNetname(), "x": pcbnew.ToMM(p.x), "y": pcbnew.ToMM(p.y)})
    out["footprints"].append({"ref": fp.GetReference(), "x": pcbnew.ToMM(pos.x), "y": pcbnew.ToMM(pos.y),
                              "angle": fp.GetOrientationDegrees(), "pads": pads})
report = os.path.join(tempfile.mkdtemp(), "drc.rpt")
pcbnew.WriteDRCReport(board, report, pcbnew.EDA_UNITS_MILLIMETRES, True)
with open(report) as f:
    out["drc"] = [l.strip() for l in f if l.startswith("[")]
print(json.dumps(out))
