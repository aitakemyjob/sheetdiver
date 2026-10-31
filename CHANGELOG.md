# Changelog

Notable changes to SheetDiver. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org). Each
[GitHub release](https://github.com/aitakemyjob/sheetdiver/releases) also lists
every merged pull request.

## [Unreleased]

### Added
- **CAD library** (preview). SheetDiver keeps your own library of
  schematic symbols, footprints and 3D models for your parts, in a folder
  you choose (Settings → CAD library), as KiCad libraries (`.kicad_sym`,
  `.pretty`, STEP models) and, if you like, an Eagle / Fusion library
  (`.lbr`, which Altium's Import Wizard also reads). Each part's **CAD**
  tab shows them per package, with where each came from:
  - **SheetDiver** draws a symbol from every pin table (supplies on top,
    grounds below, outputs on the right) and a footprint and 3D model for
    standard outlines (SOIC, TSSOP, MSOP, SOT-23, 0402–1206), with
    IPC-7351 nominal pads.
  - **Your agent** draws the rest from the datasheet: it reads the package
    outline's dimension table and the recommended land pattern
    (`draw_footprint`: gull-wing, QFP, DFN, QFN, chip and BGA packages,
    exposed pads with paste windows and thermal vias), lays out the symbol
    the way an engineer reads it (`draw_symbol`), and looks at both
    (`get_cad_image`, with the pitch, row span and pad size dimensioned)
    to compare with the drawing.
  - **You** edit anything in KiCad. Every sync reads the library back first:
    a symbol or footprint you changed (not merely re-saved) is kept as you
    left it, shown in the app, carried into the Eagle library, and never
    redrawn until you reset it; a 3D model you replaced is left alone.
  - **Through-hole and custom packages.** DIP, pin headers and SIP,
    TO-92 and TO-220 style inline packages, radial and axial parts, with
    holes sized from the lead (IPC-2222), square pin 1 pads and 3D leads
    through the board; and `custom` packages drawn pad by pad from their
    drawing (connectors with shield tabs and locating holes, modules,
    power packages), with plated and bare holes.
  New MCP tools: `get_cad`, `get_cad_image`, `draw_symbol`,
  `draw_footprint`, `reset_cad` and `sync_cad_library`.

### Fixed
- The block diagram no longer fails with "Cannot read properties of
  undefined (reading 'pins')" when a BOM entry with several designators
  (`Q1,Q4`) is drawn as transistors (#107).
- When the block diagram can't be laid out, the rest of the board is still
  drawn: the parts the layout trips on (with the passives drawn by them)
  are left off, and a message above the diagram names each with the error.
  When nothing can be drawn, the message names the parts that fail even
  on their own. The agent sees both:
  `get_connections` reports the failure (instead of "not drawn since the
  last change") or the part left off, `get_diagram_image` says why there's
  no picture, and `set_connections` with `show` waits for the drawing and
  says what went wrong (#134).
- A transistor's name on the block diagram is written beside its symbol,
  not across its drain or collector wire, and pins the pin table spells out
  ("Gate", "Drain") are labelled with their letter (G, D) so they don't
  crowd the symbol; the full name is the tooltip.
- **Passives support load switches and eFuses.** A part categorized
  `load-switch` (power muxes, ideal diodes) or `efuse` (hot-swap) was
  drawn as a two-terminal switch or fuse, so a capacitor or resistor
  couldn't be said to support it ("not another passive") and was hung
  by a guessed IC instead. Power-path, analog-switch and multiplexer
  categories are now ICs, and the fuse symbol matches the word fuse
  only. (#77)
- **A group set with a refused support is kept.** `set_connections`
  dropped a whole assignment when its `supports` was refused (another
  passive, a name that isn't on the board, or a load switch before #77),
  so the group, subgroup and cell given with it were lost and the passive
  was drawn in its part's group, listed as "(its part's)". The rest of
  the assignment is now saved, and the support it had stays. A passive
  put in a group that has no parts (so no frame), hanging off a part in
  another, was drawn in no frame, or not at all on a board with groups;
  it's now drawn with the part it hangs by, and the reply says so and
  how to put it in its group. (#82)

## [0.2.1] - 2026-10-29

### Added
- **Export a project** (`sheetdiver project export PROJECT`): a `.sdproj`
  file (a zip) with the project's rails, BOM, nets, blocks, connections,
  groups, diagram positions, calculations, notes, the SPICE models its
  parts and tests use, and every test with its variations and runs (their
  results too, so the charts show without running again). Each part comes
  with its datasheet's hash, revision and source URL, for an import to
  check the library and fetch what's missing; no PDFs are carried.
  Entries are named by designator and blocks by name, never by library
  ID. (#115)
- **Import a project** (`sheetdiver project import FILE`): each part is
  found in the library by its exact part number or orderable code (never
  a family or prefix match, which could be another value of a capacitor
  series), or joins a datasheet the library already has with the same
  hash, or has its datasheet downloaded from where the project's came
  from, else by part number. A datasheet that doesn't print the part
  number (a series, a JLCPCB copy) gets the part attached as the project
  knew it. A part that can't be had joins as "no datasheet", so the BOM
  stays complete. Then the project is made with everything in it, its
  tests' runs with their results; a name that's taken gets " (2)". The
  report says per part whether it was in the library, imported, there in
  another revision, or without a datasheet, and which SPICE models were
  kept as the library has them. A file that holds anything but the
  project and its runs' files, or comes from a newer SheetDiver, is
  refused, and an import that fails leaves nothing behind. (#115)
- **Export and import projects in the app and over MCP.** A project's
  **Export** button saves its `.sdproj` (through the save dialog in the
  desktop app, as a download in the browser); **Import → Project** takes
  one, with a name and whether to fetch missing datasheets, and shows
  what happened to each part. Agents have `export_project` (to a folder or
  a new `.sdproj` file; nothing is overwritten) and `import_project` (a
  path or the file's bytes). (#115)
- **Graphs with a title and no figure number.** Linear Technology's
  Typical Performance Characteristics are a grid of plots with a bold title
  of one or two lines over each and no "Figure n"; every plot of the
  section with a title right above it is now a figure named by the title,
  a graph with its axes read from a "y vs x" title, or a waveform for a
  scope shot. Plots stacked so close that they merge are split at the
  titles between them. The LTC3536's 24 performance graphs: all found,
  44 figures in all.
- **Pins from Linear Technology's prose pin functions.** A heading per pin
  with the numbers of several packages slash-separated ("VIN (Pin 7/Pins
  8, 9)", "PGND (Exposed Pad Pin 11/Pin 5, Exposed Pad Pin 13)") now gives
  each package its pins, named from the "(DFN/MSOP)" list under the
  heading, with the exposed pad described as one; headings are read
  wherever they stand in a line, since two columns share baselines. The
  LTC3536 joins the corpus (24 pins in two packages, ten order codes, its
  electrical values); what it still misses is #103.
- **`sheetdiver serve -mcp-port N`** serves MCP on a port of its own, so a
  service can give the UI and the agents different ports: the UI port then
  has no `/mcp`, and both ports answer `/healthz` (point the stdio bridge
  at the MCP port: `sheetdiver mcp -port N`).
- **`scripts/headless-update.sh`** (`make headless-update`) updates a
  server from source: pulls the checked-out branch, builds the frontend and
  the headless binary, swaps it in (`~/.local/bin/sheetdiver-headless`, or
  `--dest`) and restarts the pm2 process that runs it (`--pm2 NAME`).
- **`sheetdiver serve -host NAME,…`** accepts requests a reverse proxy
  passes on under its public names, which the server otherwise refuses as
  foreign hosts; the proxy has to bring the authentication, since
  SheetDiver has none.
- **Circuit simulation with ngspice (preview).** Agents set up tests
  of a project — any number, each one question they judge worth checking
  (inrush, start-up, load steps, loop gain, corners, one part on a test
  bench) — and try each in variations with `simulate`: the circuit as the
  schematic is at the time, or an alternative, with what changed, the part
  models it uses (`record_spice_model`) and the conditions it runs in
  (parameter values, temperatures). Variations are added, not replaced, so
  a test keeps its history as the design moves. SheetDiver checks each
  netlist (no `.control`, `.include` or file paths), runs each condition
  with the ngspice set in Settings → Simulation (or found on the PATH or
  in its usual place on Windows, macOS and Linux) in its own folder with
  a time limit, and keeps the results with the test. The agent writes up
  a test with `present_simulation`: a summary, charts comparing
  variations and conditions, targets that pass or fail for every
  variation in every condition, a note per variation, and conclusions,
  shown in the project's new **Simulations** tab with interactive charts
  (a crosshair reading every line, AC as magnitude and phase on a log
  frequency axis, a table of the values). (#87)

### Fixed
- **Figures inside a page-sized form.** Linear Technology sets each page
  as one form spanning most of it, and a caption's figure was the whole
  page, so none of the LTC3536's 16 numbered figures was found; a form
  spanning most of the page is now a container, not a drawing, judged
  before the caption's column is cut out of it, and on a two-column page
  a caption's figure is looked for in its own column. Bode plots count as
  graphs. The LTC3536 now gives all 16 numbered figures and its four
  section figures.

## [0.2.0] - 2026-10-25

### Fixed
- **Orderable part numbers find their part.** `MAX17048G+T10` found
  nothing although the MAX17048 was in the library: its datasheet title
  names "MAX17048/MAX17049", which was stored as one part number. Titles
  joining part numbers with a slash now give one part each ("MAX17048/9"
  too), parts stored that way before still match either number, and an
  ordering suffix after `+` or `#` (lead-free, tape and reel) is ignored
  when looking a part up. The extractor version is now 6, so re-extraction
  is offered. (#83)
- **The agent sees a parts catalogue downloaded after it connected.**
  When the MCP server was serving the library on its own (the app not
  running when the agent connected), it opened the catalogue folder once
  and kept saying the catalogue wasn't downloaded until it was restarted.
  The catalogue is now opened for each query, so every process sees it as
  it is: downloaded, replaced or removed by the app. Nothing holds the
  file between queries either, so on Windows a download or removal no
  longer fails because another process has the catalogue open. (#70)
- **An import says when pins are missing.** The extractor reads how many
  pins a datasheet gives each package ("12-Pin VQFN-HR", "8-pin TDFN",
  "SOT-23-6", "QFN32") and the import result says when it read none or
  fewer ("only 4 of 32 pins were read: … record the rest with
  record_pins") instead of a bare count. A small part with no pin table
  gets its pins from its pinout drawing ("1 Gate", "2 Source", "Drain 3"
  on the NDS331N; the "D", "S", "G" beside the leads on its older Rev. 5,
  which gives no numbers at all, so those pins are recorded unnumbered
  and the import says so), and an exposed pad the table doesn't number is
  numbered after the last pin (the TPS63900's thermal pad is 11), its
  description saying the number is SheetDiver's. (#75)
- **Pin tables with a column per package or per part are read.** The
  TPS2121's table (a WCSP column for the TPS2120 and a VQFN-HR one for
  the TPS2121, under three header rows) and the MAX17048's (TDFN and WLP,
  under "Pin/Bump Descriptions") gave no pins; the TPD2E2U06's DRL and DCK
  columns came out merged into the names, with the header row stored as
  a pin. Each package now gets its own pins, labelled with the package
  (and the part it's for), and a project part gets its own package's
  pins (else the first package's, never all of them mixed). A pin listed
  under several numbers ("4, 5, 6", "B1, B2, C1", "1, 2, 3, exposed
  thermal pad") is a pin for each, and numbers set on the line above or
  below a pin's name belong to it. (#76)
- **Imports get the right category.** Fuel gauges, TVS and surge
  protection devices and eFuses have categories of their own (and power
  multiplexers count as load switches), so a MAX17048 is no longer an
  MCU because it talks to a "host microcontroller", a TVS0500 a sensor,
  or a TPS2595 a load switch. What a datasheet says a part doesn't need
  or has inside ("no external MOSFET", "internal PMOSFET") no longer
  makes a charger a MOSFET. Re-extraction is offered. (#78)
- **An inductor across a converter's LX pins is drawn properly.** A
  switch node (LX, LX1, SW2, PH) is an output whatever its pin table
  types it, so it sits on the right of the symbol: the TPS63900's LX1 and
  LX2, typed P, went on top with the supplies, and the inductor drawn
  across them folded up with both nets into one end. A passive is only
  drawn across two pins on the same left or right side now; across any
  other two, it hangs off one with the other net named at its far end. (#86)

## [0.1.1] - 2026-10-24

### Added
- **Schematic symbols on the block diagram.** A part with a pin table in
  its datasheet is drawn as a symbol generated from it: every pin, with
  its number outside the body and its name inside, on a side by what it
  is (supplies on top, grounds and the thermal pad along the bottom,
  outputs on the right, inputs and everything else on the left), in
  pin-number order. Wires attach to the pin ends; unconnected pins are
  dimmed, and **All pins / Connected pins only** on the diagram hides
  them (but supplies and grounds) on a big microcontroller. Pins on one
  net next to each other (VDD 24, 36 and 48) share a wire with a jog to
  each. A block without a pin table is still a box of its connected
  pins. Two-terminals are drawn with their standard symbol (ANSI:
  zigzag resistor, plates, humps, triangle and bar…) hanging off their
  nets, with the right terminal at each end: as the connections name
  the pins, else by default for the shape (a diode conducts into
  ground, a Zener or TVS clamps the rail with its cathode on it, a
  polarised capacitor's + is on the net), the other way round when the
  far end is a rail. New block kinds for them: capacitor-polarised,
  schottky, zener, led, tvs, fuse, ferrite, crystal, battery, switch;
  and mosfet-n, mosfet-p, npn, pnp, drawn as transistors with their
  pins at G, D, S or B, C, E. A BOM entry's category maps to its symbol
  the same way, so a part categorised LED or crystal is drawn as one.
  The connections text for agents names each part's shape and each
  passive's symbol and terminals. A resistor or capacitor from the
  library shows its value under its name, from its role ("BOOT0 10k
  pull-down") or its part's description. (#74)
- **Wired like a schematic.** Inside a group, what stays wired at a pin
  is what a reader needs there: a crystal and its load capacitors, a
  series resistor, the pin's own pull-up or capacitor. A rail nothing in
  the group drives (3V3 in the MCU group, made in the Power group) isn't
  wired at all: each pin on it gets a power port with its name, and its
  decoupling capacitors stand in a row beside the part under the rail's
  name. A signal reaching three parts, or two parts more than a column
  apart, is a net label at each pin instead of a wire round everything
  between. Switches, buttons, LEDs and batteries stand aside under their
  net's name. Passives sit beside the pin they feed, in pin order, and
  the layout keeps every node level with what it's wired to, so the
  drawing reads left to right instead of down a tangle. (#74)
- **Functional groups on the block diagram:** put parts in groups (Power,
  MCU, Sensors) in the Connections tab, or have your agent do it. Each
  group is drawn in its own frame with its description and notes. Wires
  stay inside a group; a net that also runs in another group ends in a
  stub with its name, so a rail shared by the whole board no longer
  draws wires across it. A board without groups is drawn as before; the
  report's Mermaid chart shows each group as a subgraph. (#72)
- **Passives drawn by the part they support:** say which part a passive
  supports (a decoupling capacitor and its IC), and it hangs by that
  part's pin and is drawn in that part's group. Alike passives on one
  rail are only merged when they serve the same part, so C4 and C5 for
  two ICs on 3V3 no longer clump together at the regulator. Left unset,
  its support is guessed: the IC on its nets (over a connector or
  module), and of several, the one it feeds rather than the one driving
  the net (a bulk capacitor between an eFuse and a regulator goes by the
  regulator's VIN); a protection diode goes by its connector. The app
  shows a guess as "auto (U2)", agents as "(supports U2, guessed)", and
  confirms each change. (#71)
- **Click a net's name to see what's on it.** On the block diagram, a
  click on a net's name (on its wire, its stub or a pull-up's flag)
  marks the net's wires and junctions in the accent colour over a soft
  band and outlines the parts and passives on it, while everything else
  fades back to a third, still readable; clicking the name again,
  Escape, or a click away (the background, or a part without moving
  it) lets go.
- **Agents can see the block diagram.** `get_diagram_image` returns a
  PNG of a project's block diagram as the app draws it, whole or one
  group's frame drawn larger, made by the app from what it shows (the
  Block diagram must be open; `show: true` opens it). The app also
  checks each drawing for what looks wrong and reports it:
  `get_connections` lists, under "Layout problems", a wire that leaves
  a pin and loops round its part, a wire through a part, parts on top
  of each other and wires of two nets crossing, by group.
- **Agents place parts by column and row.** `assign` takes a `column`
  and `row` for a part or passive: its place in its group's grid of
  parts, from 0 at the left and the top. The layout keeps it there
  where the wiring allows, using ELK's interactive layering: a part
  can't share a column with one it's wired to directly, nor go past it
  (a subgroup off a pin on a part's left stays left of the part rather
  than looping its wires round it). The app reports where it drew each part, and
  `get_connections` shows each group column by column, with each place
  asked for and whether it was kept ("drawn there", or where it went
  instead); after a change it says it hasn't been drawn yet. Pixel
  `positions` stay for what the app saves when a part is dragged.
- **A passive named into a subgroup is drawn once.** One on a bottom pin
  (a regulator's VIN capacitor) was drawn in its subgroup and again
  under the pin.
- **The block diagram sits on a 10 px grid.** Every pin end, wire
  corner, passive end, ground symbol and frame corner is on it, so pins
  that face each other line up and their wire is straight, and parallel
  wires keep even spacing. A dragged part moves in grid steps and is
  saved on the grid. Symbol sizes are multiples of the grid (a pin
  every 30, passives 40 long), and a part across two pins (an inductor
  between LX1 and LX2) now has its ends level with them.
- **Groups can stack.** A group can be drawn below another, in that
  group's column (`below` in `set_connections`' `groups`, or the "below"
  picker on each group in the Connections tab): Clock under MCU, Debug
  under Power. Groups without one still go left to right in power-flow
  order; a column is as wide as its widest frame. A group set below one
  that doesn't exist, or that would close a loop, is reported and drawn
  in a column of its own, and removing a group puts those below it back
  in their own. The connections text says "[drawn below MCU]".
- **Delete a project** from its page (the button next to Report) or
  with the new `delete_project` tool, by name only: its BOM, rails,
  connections, groups, calculations and notes go; the parts stay in the
  library.
- **Every passive is wired from what it's placed by:** an IC pin, a
  connector pin, or the previous passive in a chain, on that pin's side
  of the part. A rail nothing in the group drives is still not wired
  between its parts, but each pin now wires its own capacitors (the
  wire named, a pin with nothing keeping its named stub) instead of
  leaving them under a flag; a TVS by its connector and a resistor by
  the output connector get their wire; switches, LEDs and batteries are
  wired like anything else. Which parts form a subgroup is now decided
  by the backend and carried on the diagram, so the connections text
  lists every subgroup the drawing shows, the automatic ones included
  ("3V3 at U2 (C4, C5)", "after L1 (C3)"), and those of parts in no
  group.
- **Subgroups within a group:** the parts hanging off one net (the
  VDD decoupling, a button and its pull-up, an LED and its resistor)
  are moved aside together and wired up as one unit in a faint frame
  inside their group, bus first, instead of each standing alone where
  it happened to land. Name a subgroup yourself (`cluster` in
  `set_connections`, the Subgroup column in the Connections tab) to pull
  more parts into one, or to split them; the reply names each part's
  group, subgroup and support. (#73)
- **Passives reached only through another passive are drawn:** the
  output capacitor after the inductor, an LED after its series resistor.
  Each chains off that passive on the net they share, in a subgroup of
  their own: the series part leads in from the pin's wire to a bus named
  after the net, with the rest hanging off it. The connections text says
  "C2 on VOUT after L1". (#73)

### Changed
- **One rule for what a pin is** (`internal/pinrole`), from its type
  column, then its name, then its description, shared by the connection
  checks, the pin tables, the symbols and extraction. Two consequences
  on existing boards: a pin named `IN` counts as a supply, and one named
  `VOUT` or `OUT` without a type counts as an output in the "two outputs
  on a net" check. (#74)
- **The block diagram is laid out by ELK** (the Eclipse Layout Kernel,
  elkjs) instead of SheetDiver's own router, which found a new way to
  tangle on each new board. Wires are routed around every part, a net's
  branches merge into a bus with junctions, labels go where they fit, and
  a pin's order on its side follows where its wire goes. A net leaving a
  group is a short stub straight out of its pin, named; a net wired in
  the group is named on its wire. Dragging a part now tells the layout
  which order you want it in among the parts beside it (left to right
  stays with the flow, and the wires are routed again) rather than
  pinning it exactly where it was dropped. *Auto layout* still forgets
  the dragged positions.
- **Windows: `sheetdiver-mcp.exe` works like `sheetdiver mcp`.** It relays
  to the open app, or serves your library itself while the app is closed,
  instead of starting SheetDiver. Claude Desktop now starts a short-lived
  copy of each server to check its protocol version, and a copy that
  opened the app (or kept Claude waiting while it did) broke that check.
  The Windows extension's *Start SheetDiver* setting is gone.
- **One SheetDiver window at a time.** Launching SheetDiver again brings
  the open window forward instead of opening a second app that competes
  for the MCP port and the library. (Apps on different settings files,
  `SHEETDIVER_CONFIG`, still run side by side.)

### Fixed
- **Subgroups named through `set_connections` are kept.** The tool
  accepted `cluster` and then dropped it before it reached the project,
  so no name was stored, echoed or drawn. It's now stored, shown in
  each assignment's line ("subgroup VDDA") and in each group's list,
  and drawn as its own frame with your name on it. A name splits what
  the automatic rule would put together (VDD and VDDA capacitors on one
  rail by one MCU), can span nets (Feedback), and can hold a part
  across two pins (an inductor, wired at both ends) or a box (a switch,
  a crystal). A wire into a named subgroup runs on in to the part it
  reaches, and a subgroup faces the part its members are placed by.
- **VCAP is a regulator output, not a supply input.** An MCU's VCAP
  (typed S in STM32 pin tables), VCORE or VREGOUT takes only a
  capacitor, so it's no longer checked as a supply pin or drawn on top.
- **Where a passive goes, as said.** A passive set to support a part
  goes by that part when the part is on its net, reaches it through an
  inductor, fuse or diode (the output capacitor after the inductor), or
  is the source of the rail; a passive's own group wins over the group
  of the part it's drawn by (C3 assigned to Power stays in Power). A
  protection diode of any kind (TVS, Zener, Schottky) is guessed by the
  connector it protects, not the IC. A rail nothing in a group drives
  that joins just two parts with nothing hanging off it is one wire,
  not a label at each pin. Series links through an inductor, ferrite,
  fuse or diode count on rails too (the U1 → 3V3 power link), while a
  pull-up's rail still goes nowhere.
- **Subgroups take chained passives and boxes.** A chained passive
  named into a subgroup brings the passive it chains off (the LED with
  its resistor); a capacitor whose net reaches no part of its group but
  through the inductor chains off it there; a connector or IC named
  into a subgroup is laid out inside it.
- **Changing designators keeps connections.** An entry given a second
  designator (C2 → C2,C4) keeps its connections on C2; renamed
  designators keep theirs one by one; collapsed to one, the first's
  stay.
- **Recorded pins stay recorded.** Re-extracting a datasheet no longer
  puts the extractor's pin rows back beside a pin table someone
  recorded (the MAX77827's garbled rows).
- **Nothing is overridden silently.** `set_connections` reports a
  support set to a part not on the passive's nets (still applied; the
  reply says when it will be drawn by it), refuses a passive said to
  support another passive, lists groups with no connected parts, and
  the assignments reply says "after R4" for a chained passive.
- **A rail's source makes its net.** A rail whose source is a part
  (`set_rail` with `source: U1`) is driven by that part on the block
  diagram even when its pin there isn't typed as an output: the power
  link from it to what it feeds is drawn, and the rail's capacitors go
  by it rather than by a connector on the rail.
- **Multi-pin protection parts are boxes.** A part with a pin table of
  more than two pins is drawn with its pins whatever its category says
  (a USBLC6 ESD array was a two-pin TVS symbol); an entry of several
  designators of one part (R1,R2 of a resistor array) stays a
  two-terminal each.
- **Renaming pins keeps their connections.** `record_pins` moves the
  connections of its part's BOM entries to the pins as they are now:
  one connected by name before the table had numbers goes under its
  number, a renamed pin keeps its connection, so a pin can always be
  disconnected by its current name or number. A connection made again
  by number moves the pin rather than doubling it.
- **A block diagram that can't be laid out says so** in place of the
  stale drawing, and `get_ui_context` tells the agent, instead of the
  diagram silently not updating.
- **Block diagram, from a tester's USB-C board**: a part's second pin of
  the same name on a net (a USB-C connector's two VBUS pins, a
  regulator's two IN and OUT pins) got a wire of its own, drawn as a
  zero-length stub with the net's name over the pin; it is now tied to
  the first like any other. A net reaching another group (a crystal's
  pins to the MCU) was put on the bottom of its box with no wire or
  label; it now leaves a side as a labelled stub. Stubs leaving a box
  are one length, sized to their labels, instead of each longer than
  the one below.
- **Imports named a part by LCSC's file id** ("2304140030") when the PDF
  itself named no part clearly: a distributor's file name
  (`2304140030_STMicroelectronics-STM32G031K8T6_C432211.pdf`) now gives
  the part number, without the manufacturer and file ids.
- **A datasheet title's ratings were taken for a part** ("1.8A/3.1A" from
  MAX77827's). The extractor version is now 5, so SheetDiver offers to
  re-extract; parts already imported under a wrong name keep it until
  renamed or removed.
- **Block diagram wiring on boards in groups**: wires ran through a part
  (a header's pin faced a part in another group, so its wire crossed the
  header), labels sat on a part's pin names when the piece of wire under
  them was too short, and wires of two nets met, looking shorted. Pins
  now face only parts in their own group; a label goes on a piece of its
  wire it fits; a rail reaches a part beyond the next column by a
  labelled stub; wires that go round the columns between their ends try
  over and under and take the way that crosses least; a gap leaves room
  for the capacitors hanging beside a part; and a passive with no room on
  its wire hangs beside the wire's vertical run instead of across
  another net.
- **A passive set to support a part is drawn by it even in series** (PA5
  → R5 → LED: R5 hung by the MCU, on its other end).
- **A debug header was drawn as a supply.** A connector no longer counts
  as bringing in a rail that a part on the board makes, or that it only
  senses (VTref, VREF, VTARGET).
- **VSS and VSSA were flagged as supply pins on ground**: a ground pin is
  never a supply pin, whatever its type or name.
- **No more "not connected to a rail" or "no load current" warnings for
  parts that draw no current**: LEDs, switches, crystals, connectors,
  transistors and passives, by category or designator (D, SW, Y, J…).
  Such categories ("LED", "Tactile switch", "Pin header") are also drawn
  as what they are instead of as ICs or connectors.
- **The Claude Desktop extension stopped working when SheetDiver was
  closed or restarted**, until it was disabled and enabled again. A stdio
  session now outlives the app: it serves the library itself while the
  app is closed and goes back to the app once it's running again, without
  the client reconnecting. A request that was running when the app closed
  gets an error saying to try it again, instead of hanging. A long request
  no longer holds up the others in the same session.
- **Linux: the Claude Desktop extension couldn't connect.** It ran
  SheetDiver through a Node launcher, which on some setups exited before
  SheetDiver answered, leaving nothing in Claude Desktop's log. The Linux
  extension now carries `sheetdiver-mcp`, a small static program that
  Claude Desktop starts directly, as on Windows: no Node, no GTK, and it
  works with any install. The `.deb`, `.rpm` and `.tar.gz` install it as
  `/usr/bin/sheetdiver-mcp`, and each Linux release has the extension as
  `sheetdiver_linux_<arch>.mcpb`. Save the extension again from the
  connect dialog (or use the release's) and reinstall it.
- **Claude Desktop's version probe broke the connection while the app was
  closed.** Claude Desktop can probe a server for the newest MCP protocol
  and then start the session on the same connection. SheetDiver answered
  the probe in the new protocol and then refused the session
  ("duplicate initialize"). Every path now speaks the protocol versions
  with the `initialize` handshake, up to 2025-11-25, as the app's HTTP
  endpoint already did.
- **MCP sessions are logged.** Each stdio session (the Claude Desktop
  extension, `sheetdiver mcp`) is recorded in `mcp.log` in your cache
  folder (`~/.cache/sheetdiver/mcp.log` on Linux): how it was started,
  each request's method and id (not their contents), and how it ended,
  so a failed connection says why.
- **macOS: the Claude Desktop extension exited at once ("version
  negotiation failed", "server transport closed unexpectedly").** Its
  launcher handed SheetDiver its own stdio, which the Node that Claude
  Desktop runs extensions with may not pass on, so SheetDiver found its
  input closed and stopped without a word. The launcher now pipes the
  messages through itself, and reports on stderr (in Claude Desktop's
  server log) why SheetDiver stopped if it does. Save the extension again
  from the connect dialog and reinstall it.
- **Claude Desktop couldn't start SheetDiver ("version negotiation
  failed").** Claude Desktop probes a server by sending a request and
  closing its input straight away, and `sheetdiver mcp` stopped without
  answering. It now answers every request it has read before it stops,
  both when it relays to the app and when it serves the library itself.

## [0.1.0] - 2026-10-21

The first release: a datasheet library for electronics design, shared with
your AI agent over MCP.

### Datasheets in, structured data out
- **Import** PDFs by drag-and-drop (files or folders), file picker, URL
  (product pages that link to the PDF too), part number (looked up at TI,
  ST, ADI/Maxim, onsemi, Nexperia and Diodes, then the parts catalogue) or
  `sheetdiver import`. Several import in parallel, followed in an import
  queue with progress and retry. PDFs are read by PDFium compiled to
  WebAssembly: no cgo, no external tools, no filesystem access.
- **Part numbers that resolve:** base parts, family patterns (`TPS56x200`,
  `STM32U545xx`), orderable codes (`TPS563200DDCR`, `TLV75733PDBVR`,
  `MCP1700T-3302E/TT`, `MAX77801EWP+T`) and unlisted ordering codes by
  prefix (`STM32U545VET6Q` → `STM32U545VE`).
- **Extraction:** the section tree (outline or headings, two-column reading
  order); every captioned figure cut out and classified (graph, schematic,
  waveform, block diagram, pinout, timing, layout, package) with axes from
  "Y vs X" captions; spec tables as min/typ/max parameters with conditions,
  section and a canonical key (`vin`, `iout`, `fsw`, `vref`, `ilim`, `iq`,
  `rdson`, `qg`…); pin tables, per package; every table as an image, with
  irregular ones flagged for the agent to transcribe; recommended
  inductors and capacitors; the datasheet revision (TI, ST, ADI/Maxim,
  Microchip, Diodes, AOS styles).
- Extraction knows the usual traps: names centred across condition rows,
  typ printed half a line off, ±% tolerances, current limits in normal
  operation vs start-up vs input, per-pin absolute-maximum rows, thresholds
  that aren't operating ranges, µ and Ω set in Symbol fonts, and output
  ratings stated only on the first page. A corpus of 19 real datasheets
  guards it (`make corpus && make test-corpus`); `sheetdiver inspect`
  shows what it finds.
- **Chinese and small-vendor datasheets** (TP4056, DW01A and the like):
  Chinese headings, spec and pin tables, uncaptioned figures and pins
  described in prose are extracted too.
- **Offline translation:** a part's text (sections, tables, parameters,
  figures) can be translated from Chinese to English inside the app, with
  an electronics glossary; non-English imports are translated
  automatically once it's set up. Translation runs locally (Mozilla's
  Bergamot engine); the model (about 55 MB, checked by SHA-256) is
  downloaded only when you ask, from Settings › Translation. Nothing is
  sent anywhere to translate.
- Section text reads as written: paragraphs, headings, lists, tables and
  figure references, instead of raw lines.
- **Datasheet revisions:** check one datasheet or the whole library for a
  newer revision, see which spec rows changed, and update in place. The
  same datasheet from another URL is recognised instead of duplicated.
  When the extractor improves, existing datasheets are re-extracted in the
  background; what you and your agent recorded is always kept.

### Library
- SQLite with full-text search over parts, pages, sections, figures,
  parameters, pins, registers, tables, recommendations and notes. Every
  value records whether it came from the extractor, the agent or you;
  recorded values win and survive re-extraction.
- Part view: key parameters, editable parameter table, figure gallery and
  editor, pins, sections, registers, tables, notes, variants and
  alternatives. Page viewer with figure and table overlays, highlighted
  parameter regions and drag-to-select. The PDF opens in your viewer.
- **Favorites and status** (preferred, approved, avoid, NRND, EOL), used by
  filters, comparisons, alternatives and project checks.
- **Compare by type:** a component type's parts side by side on the
  parameters that matter for it, sorted and filtered with units
  ("VIN ≥ 24 V, IOUT ≥ 3 A"), every value linked to its page; CSV export.
- **Alternatives:** library parts that could replace a part, compared key
  by key in the direction that matters; drop-ins first.
- **Delete from the library:** a confirmation lists what goes: the
  datasheet with all its parts, what you and agents recorded for it,
  notes, and the projects that use the parts (kept there without a
  datasheet, or removed with them, your choice). Agents can't delete.

### Parts catalogue (JLCPCB / LCSC)
- One click downloads the open [jlcparts](https://github.com/yaqwsx/jlcparts)
  dataset (about 2.5 million parts, republished from JLCPCB three times a
  day; about 200 MB), searched offline with a reminder to refresh it.
- Browse by category, search by part number or specs, and filter on each
  category's own parameters (Drain to Source Voltage ≥ 30V, RDS(on) ≤ 10mΩ,
  Type = N-Channel; supply ranges compare as "accepts"), with stock, price,
  JLC basic/extended library and the parameters as sortable columns.
- **Package filter** (several at once: SOT-23, SOT-23-3…), text filters
  with several values, and = as well as ≥ and ≤ on numbers (Resistance =
  10k matches 10.0kΩ, not 10.2kΩ; ranges match when they contain the
  value), with the category's common values suggested. Each filter's
  values follow the other filters, with counts, so after "VDS ≥ 30 V" the
  package list shows only packages that still have matching parts.
- Open a part at LCSC or JLCPCB from its details or its LCSC code.
- Tick parts to import their datasheets. Part-number lookups and BOM import
  fall back to its datasheet links.

### Projects and design
- **Projects** for a board: BOM with designators, rails and currents;
  per-rail load including regulator input currents derived from their
  output loads; checks against every part's absolute-maximum and
  recommended limits, regulator ratings (a current limit is only a
  ceiling), discouraged parts and parts without a datasheet.
- Chargers draw the charge current you set (none set: no charging; a
  linear charger draws the larger of that and its load, a power-path
  charger both, up to its input limit). Parts in line on a rail (a load
  switch, a protection IC) count as loads, loops in the power tree are
  reported, rails can carry other loads, and each rail shows its ripple
  budget: how much ripple it can carry before a part on it leaves its
  recommended input range.
- **Block diagram, pins and connections:** each project has a block
  diagram drawn from its connection table: parts with their pins, wires
  labelled with their nets (a bus with junctions where a net joins
  several parts), passives with their values hanging off the pins and
  nets they serve, each ground connection to a small ground symbol, and
  pull-ups pointing to their rail. No wire crosses another net's wire,
  and nothing is drawn on one. Zoom, pan, drag parts and passives,
  export as SVG. A pin table per part lists every pin with what it's
  connected to. The connection table is checked against the parts' pin
  tables: pins that don't exist, ground or supply pins on the wrong net,
  and pins left open that must be connected (supply, ground, exposed
  pads, and pins the datasheet says mustn't float). One designator of a
  BOM line with several ("R1" of "R1,R2") can be connected on its own.
  Agents can draft it all (`set_connections`).
- **BOM import** from CSV/TSV (KiCad, Altium, JLCPCB, LCSC codes): parts
  found in the library or fetched by part number, with JLC availability.
- **Design-review report** as Markdown (Report button, `sheetdiver report`).
- **Calculators:** buck, boost, LDO, MOSFET gate drive, gate bias divider
  and resistor divider, with inputs filled from the datasheets (with page
  references) and project rails, and results saved to the project.

### For AI agents
- A built-in MCP server (streamable HTTP on 127.0.0.1, or `sheetdiver mcp`
  over stdio) with 46 tools: search and read the library, see figures,
  pages and tables as images, record parameters, figure descriptions,
  tables, pins, registers, recommendations and notes, import datasheets and
  BOMs, search the parts catalogue, find alternatives, manage projects, run
  calculators and export reports. Settings switch agents off, make them
  read-only or forbid imports; their changes are marked.
- **Follow agent:** what the agent shows opens in front of you: parts,
  pages, figures, projects with the rail or BOM entry it means
  highlighted, parts compared side by side with the best and worst values
  marked and its pick starred, catalogue candidates, and calculators with
  its inputs. Its explanation sits beside them, with links that open
  parts, pages, figures and sections. It can also walk you through
  several steps (Alt+← / Alt+→).
- **Connect an agent** (the MCP pill) has the setup for Claude Code,
  Claude Desktop, Codex, Cursor, VS Code, Gemini CLI and Windsurf, and
  saves each one's config file. For Claude Desktop it saves an extension
  (`sheetdiver.mcpb`; install it from Settings › Extensions › Advanced
  settings › Install Extension…).
- On Windows a small console program, `sheetdiver-mcp.exe`, is installed
  beside the app for Claude Desktop and other stdio clients; it starts
  SheetDiver when it's closed (an extension setting). Releases also have
  the extension as `sheetdiver_windows_amd64.mcpb` for installer
  installs.

### Apps and packaging
- Desktop app for macOS (universal), Windows and Linux (x64 and ARM64; .deb,
  .rpm, .tar.gz), plus `sheetdiver serve` (headless, the same UI in your
  browser) for every platform. Ten themes shared with CodeDiver.
- Releases ship checksums, an SPDX SBOM, signed build provenance, and the
  third-party notices (`sheetdiver licenses`).

[Unreleased]: https://github.com/aitakemyjob/sheetdiver/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/aitakemyjob/sheetdiver/releases/tag/v0.2.1
[0.2.0]: https://github.com/aitakemyjob/sheetdiver/releases/tag/v0.2.0
[0.1.1]: https://github.com/aitakemyjob/sheetdiver/releases/tag/v0.1.1
[0.1.0]: https://github.com/aitakemyjob/sheetdiver/releases/tag/v0.1.0
