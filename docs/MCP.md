# MCP tool reference

SheetDiver serves the [Model Context Protocol](https://modelcontextprotocol.io)
at `http://127.0.0.1:47480/mcp` (streamable HTTP), and over stdio through
`sheetdiver mcp`. Every call appears in the app's agent activity feed.

Conventions:

- A **part** argument accepts a base part number (`TPS563200`), an orderable
  code (`TPS563200DDCR`), an unlisted ordering code that extends a known part
  (`STM32U545VET6Q` → `STM32U545VE`), a family pattern (`TPS56x200`) or an id.
- Pages are **1-based**. Regions are in PDF points from the top-left of the
  page (US Letter is 612 × 792).
- Calculator inputs and outputs are **SI** (V, A, Hz, s, H, F, Ω, C).
- Data the agent records is marked `agent`, kept when a datasheet is
  re-extracted, and preferred over automatic extraction.
- The user can switch agents off, make them read-only, or forbid imports in
  Settings → Agent access.

## Library

| Tool | What it does |
|---|---|
| `library_overview` | Datasheets, parts by category, projects, and what the user has open. |
| `get_ui_context` | The part, page, figure or project the user is looking at, and any selected text. |
| `search` | Full-text search across parts, page text, sections, figure captions and descriptions, parameters, pins, registers, recommendations and notes. |
| `list_categories` | Component types with part counts and the key parameters compared for each. |
| `compare_parts` | (Optionally only starred parts or one status.) One type's parts side by side on key parameters (min/typ/max in SI), filtered by requirements such as `vin >= 24 V`, `iout >= 3 A`, `iq <= 1 µA` and sorted. A `>=` filter tests the highest the part can do (max), `<=` tests typ. |
| `list_parts` | Parts filtered by part number/variant/description, category or manufacturer. |
| `get_part` | Overview: datasheet, sibling parts, orderable variants, best value per key parameter (with page), sections, figure counts, recommended components, projects, notes. |
| `get_parameters` | Spec parameters (min/typ/max, unit, conditions, section, page), filtered by key, section or text. |
| `get_section` | A section's text by kind (`pins`, `registers`, `layout`, `design_procedure`, `component_selection`, `typical_application`, …), title or number; without one, the outline. |
| `read_pages` | Plain text of up to 10 pages. |
| `get_page_image` | A page or a region of it as a PNG image. |
| `list_figures` | Figures with number, page, kind (`graph`, `schematic`, `waveform`, `block_diagram`, `pinout`, `timing`, `layout`, `package`), caption, axes and recorded description. |
| `get_figure` | A figure as an image, with what is known about it and the text inside it. |
| `get_tables` | Detected tables, or one table as an image with its cells. Tables flagged IRREGULAR (matrices, grouped rows, multi-row headers) have unreliable cells; read the image. |
| `get_pins` | The pin table. |
| `get_registers` | Recorded registers and the register-map sections to read from. |
| `show` | Show a part, page (optionally highlighting a region), figure, section, project or search in the app. |

## Recording what the agent learns

| Tool | What it does |
|---|---|
| `import_datasheet` | Download and import a datasheet: by `part_number` (tries TI, ST, ADI, onsemi, Nexperia and Diodes datasheet URLs), from a `url` (a PDF, or a product page linking to one), a local `path`, or `data_base64` for a PDF the agent fetched itself because the site blocks scripted downloads. `reextract` redoes extraction. |
| `import_datasheets` | Import many datasheets at once, in parallel: part numbers, URLs, paths or a folder (optionally recursive). One line per import with its outcome. |
| `import_bom` | Build a project from a BOM (CSV/TSV text or file from KiCad, Altium, JLCPCB…): parts found in the library or fetched by part number; unfetchable parts join as "no datasheet"; lines without a part number are skipped. |
| `update_part` | Correct part number, manufacturer, description, category, package, tags; add orderable variants or more part numbers on the same datasheet; set family patterns (`STM32U545xx`); star it (`favorite`) or set its `status` (preferred, approved, avoid, nrnd, eol). |
| `record_parameters` | Add or update parameters with key, section and page; `replace_auto` drops automatic rows for the same key; `delete` removes wrong rows. |
| `describe_figure` | Record what a figure shows: kind, title, axes with ranges, conditions, description, curves. |
| `record_table` | Save a clean transcription of a table read from its image (header with units, one row per entry; matrices flattened to one row per combination). Replaces the automatic cells, makes them searchable, survives re-extraction. Can also add a table the extractor missed (page + region). |
| `record_pins` | Replace the pin table (or one package's pins). |
| `record_registers` | Add registers with address, width, reset, access and bit fields. |
| `record_recommendations` | Recommended components: role, value, part number, manufacturer, conditions, page. Regular "recommended inductors/capacitors" tables are turned into recommendations automatically. |
| `add_note` | A markdown note on a part, figure, parameter, datasheet or project. |
| `list_notes` | Notes on a target, or all notes. |

## Projects

| Tool | What it does |
|---|---|
| `list_projects` | Projects with part and rail counts. |
| `create_project` | Create a project (optionally the active one, which project arguments default to). |
| `get_project` | BOM, rails with load (direct loads plus regulator input currents derived from their output loads), capacity and power, input power, checks of every rail against each part's absolute-maximum and recommended limits, saved calculations, notes. |
| `set_rail` | Add, update or delete a rail: nominal/min/max voltage, current limit, source. |
| `add_to_project` | Place a part with designator, quantity, role, supply rail, output rail and load current. |
| `update_project_part` | Change or remove a BOM entry by id or designator. |

## Calculators

| Tool | What it does |
|---|---|
| `list_calculators` | Calculators, their inputs, and the canonical parameter keys. |
| `calculate` | Run `buck`, `boost`, `ldo`, `gate_drive`, `gate_divider` or `divider`. With `part`, inputs are filled from the datasheet (switching frequency, reference voltage, current limit, minimum on/off time, gate charge, …); with `project`, from the part's rails. The result lists every filled value with its page, outputs with formulas, checks against device limits, and suggestions. `save` stores it in the project. |

## Canonical parameter keys

Calculators and project checks find values by key. `list_calculators`
prints the full list; the common ones are `vin`, `vout`, `iout`, `fsw`,
`vref`, `ilim` (steady-state peak/valley limit; `ilim_startup` and `ilim_in`
are the start-up and average input limits), `ton_min`, `toff_min`, `iq`, `uvlo`, `rdson`, `rdson_hs`,
`rdson_ls`, `vds`, `vgs`, `vgs_th`, `qg`, `qgd`, `ciss`, `vdropout`,
`rth_ja` and `tj`. A key's section matters: `vin` under `abs_max` is the
rating, under `recommended` the operating range.

## Example session

```text
> Import the TPS563200 datasheet and design a 12 V → 3.3 V, 2 A supply with it.

import_datasheet {part_number: "TPS563200"}
get_part {part: "TPS563200"}                       key parameters, sections
get_figure {part: "TPS563200", figure: "8-16"}     the reference schematic, as an image
record_recommendations {part: "TPS563200", …}      inductor and capacitor values read from it
calculate {kind: "buck", part: "TPS563200",
           inputs: {vinMin: 10.8, vinMax: 13.2, vout: 3.3, iout: 2}, show: true}
```
