// Mirrors of the Go JSON contract (internal/library, internal/core,
// internal/calc, internal/config). Keep in sync when the Go structs change.

export interface Rect {
  x0: number;
  y0: number;
  x1: number;
  y1: number;
}

export interface Settings {
  theme: string;
  fontSize: number;
  mcpPort: number;
  mcpEnabled: boolean;
  followAgent: boolean;
  agentWrites: boolean;
  agentImport: boolean;
  libraryDir: string;
  /** Days before the parts catalogue reminder: 0 = 14, −1 = never. */
  catalogRemindDays?: number;
  /** The ngspice program simulations run with; empty finds it. */
  ngspicePath?: string;
  /** The folder the CAD library is kept in (empty: in the SheetDiver library). */
  cadLibraryDir?: string;
  /** The CAD library's name in the CAD tools (empty: SheetDiver). */
  cadLibraryName?: string;
  /** The formats it is kept in (kicad, eagle); empty is KiCad, ["none"] none. */
  cadFormats?: string[];
}

export interface Config {
  settings: Settings;
  activeProject: string;
}

export interface MCPStatus {
  running: boolean;
  url: string;
  error?: string;
}

/** How a client launches SheetDiver's stdio MCP bridge. */
export interface StdioCommand {
  command: string;
  args: string[];
}

export interface AppState {
  version: string;
  platform: string;
  config: Config;
  libraryDir: string;
  mcp: MCPStatus;
  /** Starts this install's stdio MCP bridge. */
  stdio: StdioCommand;
  canPickFile: boolean;
  canOpenFile: boolean;
  canSaveFile: boolean;
  keys: Record<string, string>;
  statuses: Record<string, string>;
  calculators: Record<string, string>;
}

export interface Datasheet {
  id: number;
  sha256: string;
  file: string;
  filename: string;
  title: string;
  description: string;
  manufacturer: string;
  pages: number;
  families: string[] | null;
  sourceUrl?: string;
  /** PDF metadata plus revision, revisionDoc/Rev/Date and previousRevision after an update. */
  meta?: Record<string, string>;
  extractor: string;
  importedAt: string;
}

export interface Part {
  id: number;
  mpn: string;
  manufacturer: string;
  description: string;
  category: string;
  package?: string;
  datasheetId: number;
  tags: string[];
  favorite: boolean;
  /** preferred, approved, avoid, nrnd or eol. */
  status?: string;
  createdAt: string;
  updatedAt: string;
}

export interface Variant {
  mpn: string;
  partId: number;
  package?: string;
  notes?: string;
  source: string;
}

export interface Line {
  t: string;
  b: Rect;
  s: number;
}

export interface Page {
  datasheetId: number;
  page: number;
  width: number;
  height: number;
  text: string;
  lines?: Line[];
}

export interface Section {
  id: number;
  datasheetId: number;
  ord: number;
  num: string;
  title: string;
  level: number;
  kind: string;
  page: number;
  y: number;
  endPage: number;
  endY: number;
  text?: string;
}

export interface Curve {
  label: string;
  conditions?: string;
  summary?: string;
}

export interface Figure {
  id: number;
  datasheetId: number;
  page: number;
  number: string;
  caption: string;
  kind: string;
  box: Rect;
  section: string;
  title?: string;
  xAxis?: string;
  yAxis?: string;
  conditions?: string;
  description?: string;
  curves?: Curve[] | null;
  appliesTo?: string[] | null;
  source: string;
  updatedAt: string;
}

export interface Table {
  id: number;
  datasheetId: number;
  page: number;
  number?: string;
  caption?: string;
  kind: string;
  section: string;
  box: Rect;
  header: string[] | null;
  rows?: string[][] | null;
  irregular?: string;
  title?: string;
  description?: string;
  source: string;
  updatedAt: string;
}

export interface Parameter {
  id: number;
  datasheetId: number;
  appliesTo?: string[] | null;
  key?: string;
  symbol?: string;
  name: string;
  conditions?: string;
  min?: number | null;
  typ?: number | null;
  max?: number | null;
  unit?: string;
  section: string;
  page: number;
  box?: Rect | null;
  raw?: string;
  note?: string;
  source: string;
  updatedAt: string;
}

export interface Pin {
  id: number;
  package?: string;
  number: string;
  name: string;
  type?: string;
  description?: string;
  page: number;
  source: string;
}

export interface RegisterField {
  name: string;
  bits: string;
  access?: string;
  reset?: string;
  description?: string;
}

export interface Register {
  id: number;
  name: string;
  address?: string;
  width?: number;
  reset?: string;
  access?: string;
  description?: string;
  fields?: RegisterField[] | null;
  page: number;
  source: string;
}

export interface Recommendation {
  id: number;
  datasheetId: number;
  appliesTo?: string[] | null;
  role: string;
  value?: string;
  mpn?: string;
  manufacturer?: string;
  conditions?: string;
  notes?: string;
  page: number;
  source: string;
}

export interface Note {
  id: number;
  targetKind: string;
  targetId: string;
  body: string;
  author: string;
  createdAt: string;
  updatedAt: string;
}

export interface Project {
  id: number;
  name: string;
  description: string;
  createdAt: string;
  updatedAt: string;
  parts: number;
  rails: number;
}

export interface KeyValue {
  key: string;
  label: string;
  param: Parameter;
  rating?: boolean;
}

export interface PartDetail {
  part: Part;
  via: string;
  matched?: string;
  datasheet?: Datasheet;
  siblings: string[];
  variants: Variant[] | null;
  keyParameters: KeyValue[] | null;
  sections: Section[] | null;
  figureKinds: Record<string, number> | null;
  counts: Record<string, number>;
  notes: Note[] | null;
  projects: Project[] | null;
  recommended: Recommendation[] | null;
}

export interface Hit {
  kind: string;
  ref: string;
  datasheetId?: number;
  page: number;
  title: string;
  snippet: string;
  score: number;
  parts?: string[] | null;
}

export interface Rail {
  id: number;
  projectId: number;
  name: string;
  vNom?: number | null;
  vMin?: number | null;
  vMax?: number | null;
  source?: string;
  currentLimit?: number | null;
  /** Loads on the rail that aren't in the BOM (amps). */
  otherTyp?: number | null;
  otherMax?: number | null;
  notes?: string;
}

export interface RailStatus extends Rail {
  loadTyp: number;
  loadMax: number;
  capacity?: number;
  capacityFrom?: string;
  /** The regulator's current limit when it has no output rating: a ceiling, not a capacity. */
  ceiling?: number;
  ceilingFrom?: string;
  sourcePart?: string;
  consumers: string[];
  power: number;
  /** Peak-to-peak ripple (volts) the rail can carry before a part on it leaves its recommended input range. */
  ripple?: number;
  /** The part and limit that set the ripple budget. */
  rippleLimit?: string;
}

export interface ProjectPart {
  id: number;
  projectId: number;
  partId: number;
  mpn: string;
  category: string;
  status?: string;
  /** False for BOM placeholders whose datasheet isn't imported. */
  hasDatasheet: boolean;
  datasheetId?: number;
  designator?: string;
  qty: number;
  role?: string;
  supplyRail?: string;
  outputRail?: string;
  iTyp?: number | null;
  iMax?: number | null;
  variant?: string;
  notes?: string;
}

export interface Check {
  level: 'ok' | 'warn' | 'error';
  message: string;
  part?: string;
  rail?: string;
  page?: number;
  /** "connections" for checks on the connection table. */
  area?: string;
}

export interface Calculation {
  id: number;
  projectId?: number;
  partId?: number;
  kind: string;
  title: string;
  inputs: Record<string, unknown>;
  outputs: CalcResponse;
  createdAt: string;
}

export interface ProjectDetail {
  project: Project;
  parts: ProjectPart[];
  rails: RailStatus[];
  checks: Check[];
  calculations: Calculation[];
  notes: Note[];
  inputPower: number;
  connections: Connections;
}

/** A net in a project's connection table. */
export interface Net {
  id: number;
  name: string;
  kind: 'signal' | 'power' | 'ground';
  bus?: string;
  voltage?: number;
  notes?: string;
  source: string;
}

/** Something off the BOM the board connects to, or a passive with its value. */
export interface Block {
  id: number;
  name: string;
  kind?: string;
  value?: string;
  notes?: string;
  source: string;
}

/** A pin of a BOM entry (or a block) on a net. */
export interface ConnectionRow {
  id: number;
  netId: number;
  net: string;
  entryId?: number;
  blockId?: number;
  /** Which of the entry's designators ("R1" of "R1,R2"). */
  ref?: string;
  pin?: string;
  notes?: string;
  source: string;
  endpoint: string;
  pinName?: string;
  pinType?: string;
}

/** What a pin does (see internal/pinrole). */
export type PinRole = 'power' | 'ground' | 'pad' | 'input' | 'output' | 'bidir' | 'analog' | 'passive' | 'nc' | 'other';

/** A symbol's outline: a rectangle with its pins, or a transistor. */
export type PartShape = 'ic' | 'mosfet-n' | 'mosfet-p' | 'npn' | 'pnp';

/** The kinds a block can be: what it's drawn as (see library.BlockKinds). */
export const BLOCK_KINDS = [
  'ic',
  'connector',
  'module',
  'resistor',
  'capacitor',
  'capacitor-polarised',
  'inductor',
  'diode',
  'schottky',
  'zener',
  'led',
  'tvs',
  'fuse',
  'ferrite',
  'crystal',
  'battery',
  'switch',
  'mosfet-n',
  'mosfet-p',
  'npn',
  'pnp',
] as const;

/** A standard two-terminal symbol. */
export type PassiveShape =
  | 'resistor'
  | 'capacitor'
  | 'capacitor-polarised'
  | 'inductor'
  | 'diode'
  | 'schottky'
  | 'zener'
  | 'led'
  | 'tvs'
  | 'fuse'
  | 'ferrite'
  | 'crystal'
  | 'battery'
  | 'switch';

/** A pin of a symbol on the block diagram, connected or not (see core.DiagramPin). */
export interface DiagramPin {
  name: string;
  /** From the pin table; none for a block's pin. */
  number?: string;
  /** Unset when unconnected. */
  net?: string;
  side: 'left' | 'right' | 'top' | 'bottom';
  role: PinRole;
  connected: boolean;
  /** A transistor pin's terminal: gate, drain, source; base, collector, emitter. */
  terminal?: string;
  /** The pin on the same net it's tied to: drawn as a jog to that pin's wire. */
  tied?: string;
  /** Has a passive across it and another pin (LX1/LX2 and the inductor). */
  bridged?: boolean;
  /** Goes to ground: drawn along the bottom, to a ground symbol. */
  ground?: boolean;
}

/** A part or connector drawn as a box, in a column along the power flow. */
export interface DiagramPart {
  id: string;
  title: string;
  mpn?: string;
  kind: string;
  shape: PartShape;
  /** pins is the whole pin table, connected or not. */
  full?: boolean;
  column: number;
  /** The group it's drawn in. */
  group?: string;
  /** The subgroup it's wired up in with the group's other parts of that name. */
  cluster?: string;
  /** Where in its group it's asked to be drawn. */
  want?: { column?: number; row?: number };
  pins: DiagramPin[];
  /** Where it was dragged to. */
  x?: number;
  y?: number;
}

/** A passive (or several alike: "C1,C2") and where it hangs. */
export interface DiagramPassive {
  id: string;
  label: string;
  value?: string;
  kind: string;
  /** Chain: after the passive `part` names, on the net they share, which reaches no part but through it. */
  mount: 'wire' | 'lane' | 'out' | 'below' | 'bridge' | 'chain';
  part: string;
  nets: string[];
  /** The part it serves: set, or guessed from its nets. */
  supports?: string;
  guessed?: boolean;
  /** The group it's drawn in (its part's). */
  group?: string;
  /** The subgroup it's wired up in (see DiagramPart.cluster). */
  cluster?: string;
  /** Where in its group it's asked to be drawn. */
  want?: { column?: number; row?: number };
  /** The standard symbol it's drawn with, and its terminals at nets[0]'s end and the far end. */
  shape: PassiveShape | string;
  ends: string[];
  /** Where it was dragged to. */
  x?: number;
  y?: number;
}

/**
 * A net's wire within one group: the parts there on it in column order,
 * and how its wire runs. Offboard: the net also runs in other groups.
 */
export interface DiagramNet {
  name: string;
  kind: string;
  group?: string;
  parts: string[];
  wire: 'between' | 'lane' | 'out' | '';
  arrow?: boolean;
  offboard?: boolean;
}

/** A functional group: a frame around its parts, with its description and notes. */
export interface DiagramGroup {
  name: string;
  description?: string;
  notes?: string;
  /** The group it's drawn under, in that group's column. */
  below?: string;
  parts: string[];
}

/** Two parts sharing nets (the report's chart, agents). */
export interface DiagramLink {
  from: string;
  to: string;
  label: string;
  kind: 'power' | 'signal';
  nets: string[];
}

/** A project's block diagram: what's drawn; the app decides where (lib/blockDiagram.ts). */
/**
 * A set of a group's parts laid out and wired up as one unit in a faint
 * frame: as named, or found by the backend's rules (internal/core/subgroups.go).
 */
export interface DiagramSubgroup {
  /** As named; "" for one a rule found. */
  name?: string;
  group?: string;
  /** "rail": passives on a rail nothing in the group drives, by one part; "chain": a passive reached only through another, which leads. */
  kind: 'named' | 'rail' | 'chain';
  /** The rail, or the net a chain hangs on. */
  net?: string;
  /** Node ids, parts and passives; a chain's lead first. */
  members: string[];
}

export interface Diagram {
  parts: DiagramPart[];
  passives: DiagramPassive[];
  nets: DiagramNet[];
  /** Groups with something drawn in them, in power-flow order. */
  groups: DiagramGroup[];
  links: DiagramLink[];
  subgroups: DiagramSubgroup[];
}

/** A project's functional group. */
export interface Group {
  id: number;
  name: string;
  description?: string;
  notes?: string;
  /** The group it's drawn under; none starts a column of its own. */
  below?: string;
  source: string;
}

/** Where a part or block belongs: its group, the part it supports, and its subgroup. */
export interface Assignment {
  node: string;
  endpoint: string;
  group?: string;
  supports?: string;
  supportsEndpoint?: string;
  cluster?: string;
  source: string;
}

export interface Connections {
  nets: Net[];
  blocks: Block[];
  connections: ConnectionRow[];
  groups: Group[];
  assignments: Assignment[];
  diagram: Diagram;
  /** Each part's pins and what they're connected to. */
  pinouts: Pinout[];
}

/** A part's (or block's) pins, for the reference table. */
export interface Pinout {
  node: string;
  title: string;
  mpn?: string;
  pins: { number?: string; name?: string; type?: string; description?: string; net?: string; netKind?: string; to: string[] }[];
}

/** An edit of the connection table and block diagram (see core.ConnectionsChange). */
export interface ConnectionsChange {
  blocks?: { name: string; kind?: string; value?: string; notes?: string; remove?: boolean }[];
  nets?: { name: string; kind?: string; bus?: string; voltage?: number; notes?: string; remove?: boolean }[];
  connect?: { net: string; part: string; pin?: string; notes?: string; remove?: boolean }[];
  groups?: { name: string; description?: string; notes?: string; below?: string; remove?: boolean }[];
  /** Unset fields are kept; "" clears one. */
  assign?: { part: string; group?: string; supports?: string; cluster?: string; column?: number; row?: number }[];
  positions?: { node: string; x: number; y: number }[];
  resetLayout?: boolean;
}

export interface CalcOutput {
  name: string;
  symbol?: string;
  value: number;
  unit: string;
  formula?: string;
  note?: string;
}

export interface CalcResult {
  kind: string;
  summary: string;
  outputs: CalcOutput[] | null;
  checks: Check[] | null;
  suggestions?: string[] | null;
}

export interface Filled {
  field: string;
  value: number;
  unit: string;
  from: string;
  page?: number;
  paramId?: number;
}

export interface CalcResponse {
  kind: string;
  part?: string;
  project?: string;
  inputs: Record<string, unknown>;
  filled: Filled[];
  missing?: string[] | null;
  result: CalcResult;
  savedId?: number;
}

export interface CalcField {
  name: string;
  type: string;
  description: string;
  required: string;
}

export interface ImportResult {
  datasheet: Datasheet;
  parts: Part[];
  existing: boolean;
  counts?: Record<string, number>;
  families?: string[] | null;
  variants: number;
  took: string;
}

/** One import in the import queue. */
export interface ImportJob {
  id: number;
  name: string;
  source: string;
  status: 'queued' | 'running' | 'done' | 'existing' | 'failed';
  stage?: string;
  done: number;
  total: number;
  error?: string;
  datasheetId?: number;
  parts?: string[];
  batch?: string;
  language?: string;
  translated?: boolean;
  started: string;
  finished: string;
}

export interface BOMRow {
  line: number;
  designators: string[];
  mpn?: string;
  manufacturer?: string;
  value?: string;
  description?: string;
  qty: number;
  part?: string;
  status: 'in library' | 'imported' | 'no datasheet' | 'skipped';
  note?: string;
  lcsc?: string;
  /** JLCPCB availability, when the parts catalogue is downloaded. */
  jlc?: { lcsc: string; library: string; stock: number };
}

export interface BOMResult {
  project: string;
  rows: BOMRow[];
  counts: Record<string, number>;
}

/** What happened to one part of an imported project. */
export interface ProjectImportPart {
  mpn: string;
  /** The library part it became, when its name differs. */
  part?: string;
  status: 'in library' | 'other datasheet' | 'imported' | 'no datasheet';
  note?: string;
}

/** The result of importing a project file (.sdproj). */
export interface ProjectImportResult {
  project: string;
  parts: ProjectImportPart[];
  counts: Record<string, number>;
  notes?: string[];
  tests: number;
  runs: number;
}

export interface Activity {
  id: number;
  time: string;
  tool: string;
  summary: string;
  error?: string;
  durationMs: number;
}

export interface Focus {
  kind: string;
  part?: string;
  datasheetId?: number;
  page?: number;
  box?: Rect;
  figureId?: number;
  sectionId?: number;
  project?: string;
  tab?: string;
  query?: string;
  calc?: CalcResponse;
  message?: string;
  /** compare: parts side by side, the keys, a note per part. */
  parts?: string[];
  keys?: string[];
  notes?: Record<string, string>;
  /** catalog: candidates by LCSC code, or a search. */
  lcsc?: string[];
  subcategory?: string;
  filters?: CatalogFilter[];
  /** What to mark: the recommended part, an LCSC code, a rail or a BOM entry. */
  highlight?: string;
  /** The agent's explanation (markdown), shown beside the view. */
  explain?: string;
  /** walkthrough: its title and steps. */
  title?: string;
  steps?: Focus[];
}

export interface CategoryInfo {
  category: string;
  count: number;
  keys: string[] | null;
}

export interface CompareFilter {
  key: string;
  op: '>=' | '<=' | '>' | '<';
  value: number;
  stat?: string;
}

export interface CompareValue {
  min?: number | null;
  typ?: number | null;
  max?: number | null;
  unit: string;
  rating?: boolean;
  conditions?: string;
  page: number;
  box?: Rect | null;
  paramId: number;
  source: string;
}

export interface CompareColumn {
  key: string;
  label: string;
  unit: string;
  have: number;
  /** Which values are better: higher, lower, close (to a target) or cover (a range). */
  direction?: 'higher' | 'lower' | 'close' | 'cover';
}

/** A part's values in a comparison. */
export interface CompareRow {
  part: Part;
  values: Record<string, CompareValue>;
}

export interface CompareResult {
  category: string;
  columns: CompareColumn[] | null;
  rows: CompareRow[];
  total: number;
  unknown?: Record<string, number> | null;
}

export interface BusEvent {
  type: string;
  time: string;
  data?: unknown;
}

export type Verdict = 'better' | 'same' | 'worse' | 'unknown';

export interface AltCheck {
  key: string;
  direction: 'higher' | 'lower' | 'cover' | 'close';
  original: string;
  candidate: string;
  verdict: Verdict;
  page: number;
}

export interface Alternative {
  part: Part;
  checks: AltCheck[];
  better: number;
  same: number;
  worse: number;
  unknown: number;
  samePackage: boolean;
  sameDatasheet: boolean;
  dropIn: boolean;
  distance: number;
}

export interface AltResult {
  part: Part;
  category: string;
  keys: string[];
  alternatives: Alternative[];
  considered: number;
}

export interface ParamChange {
  kind: 'changed' | 'added' | 'removed';
  key?: string;
  name: string;
  section: string;
  conditions?: string;
  before?: string;
  after?: string;
  page: number;
}

export interface UpdateCheck {
  datasheetId: number;
  filename: string;
  source: string;
  current: string;
  latest?: string;
  upToDate: boolean;
  changes?: ParamChange[];
  counts?: Record<string, number>;
  recorded: number;
  error?: string;
}

/** A part in the JLCPCB/LCSC catalogue. */
export interface CatalogItem {
  lcsc: string;
  mpn: string;
  manufacturer: string;
  package: string;
  category: string;
  subcategory: string;
  description: string;
  datasheet: string;
  stock: number;
  library: 'basic' | 'preferred' | 'extended';
  price: number;
  attributes?: Record<string, string>;
  /** The library part it resolves to, when already imported. */
  inLibrary?: string;
}

export interface CatalogProgress {
  stage: 'starting' | 'downloading' | 'extracting' | 'checking' | 'done';
  done: number;
  total: number;
}

export interface CatalogStatus {
  installed: boolean;
  updating: boolean;
  parts: number;
  inStock: number;
  published: string;
  downloaded: string;
  sizeBytes: number;
  source: string;
  ageDays: number;
  remindDays: number;
  stale: boolean;
  progress?: CatalogProgress;
  error?: string;
}

/** A JLCPCB category and subcategory with part counts. */
export interface CatalogCategory {
  category: string;
  subcategory: string;
  parts: number;
  inStock: number;
}

/** A parameter of a catalogue category, for filtering. */
export interface CatalogParam {
  key: string;
  kind: 'number' | 'text';
  count: number;
  unit?: string;
  min?: number;
  max?: number;
  values?: { value: string; count: number }[];
}

/** A catalogue parameter filter: input is typed ("30V", "10mΩ", "N-Channel"). */
export interface CatalogFilter {
  key: string;
  op: '>=' | '<=' | '=';
  input: string;
  /** For "=": the values to accept (any of them), e.g. several packages. */
  values?: string[];
}

/** What deleting a part would remove (shown for confirmation). */
export interface DeletePlan {
  datasheet?: Datasheet;
  parts: { part: Part; projects?: string[] }[];
  recorded: Record<string, number>;
  notes: number;
  inProjects: boolean;
}

export interface DeleteResult {
  datasheet?: string;
  deleted: string[];
  kept?: string[];
}

/** An offline translation model (see internal/translate). */
export interface TranslationModel {
  from: string;
  to: string;
  label: string;
  version: string;
  installed: boolean;
  downloading: boolean;
  sizeBytes: number;
}

/** The translation models, and the progress or error of a download. */
export interface TranslateStatus {
  models: TranslationModel[];
  progress?: { model: string; done: number; total: number };
  error?: string;
}

// ---- simulations (internal/core/simulations.go, internal/library/simulations.go)

/** The ngspice simulations run with, or why there's none. */
export interface NgspiceState {
  path?: string;
  version?: string;
  /** settings, PATH or default. */
  from?: string;
  error?: string;
}

/** A run's state: queued → running → done | failed | cancelled; stale after the simulation changes. */
export type RunState = 'queued' | 'running' | 'done' | 'failed' | 'cancelled' | 'stale';

export interface SimModel {
  part: string;
  name: string;
}

/** A condition a variation runs in: parameter values and temperature (a corner, a load). */
export interface SimCondition {
  name: string;
  params?: Record<string, string>;
  temp?: number;
}

/** A chart the agent asked for. */
export interface SimPlot {
  title: string;
  /** The analysis's plot: tran1, ac1, dc1… */
  plot: string;
  signals: string[];
  /** Which variations it compares (none: the latest). */
  variations?: string[];
  /** Which conditions (none: all). */
  conditions?: string[];
  /** For AC: mag (dB) or phase; none shows both. */
  part?: string;
  xLog?: boolean;
  yLabel?: string;
  note?: string;
}

/** A target a measurement should meet. */
export interface SimCheck {
  measure: string;
  min?: number;
  max?: number;
  unit?: string;
  why?: string;
}

/** The agent's write-up of a test across its variations. */
export interface Presentation {
  summary: string;
  plots: SimPlot[];
  checks: SimCheck[];
  /** What the agent took from each variation. */
  notes?: { variation: string; text: string }[];
  conclusions?: string;
  standIns?: string[];
  updated?: string;
}

/** A test: one question, tried in variations. */
export interface Simulation {
  id: number;
  projectId: number;
  name: string;
  kind?: string;
  purpose?: string;
  /** The part it tests on its own; empty for the board. */
  target?: string;
  presentation?: Presentation;
  source: string;
  created: string;
  updated: string;
}

/** A try of a test: the circuit then, or an alternative, with what changed. */
export interface Variation {
  id: number;
  simulationId: number;
  name: string;
  change?: string;
  netlist: string;
  models: SimModel[];
  conditions: SimCondition[];
  source: string;
  created: string;
  updated: string;
}

export interface RunPlot {
  id: string;
  name: string;
  complex?: boolean;
  points: number;
  vectors: { name: string; kind: string }[];
}

export interface RunMeasure {
  name: string;
  value?: number;
  at?: number;
  error?: string;
}

/** A run of a variation in one condition. */
export interface SimRun {
  id: number;
  variationId: number;
  variation: string;
  condition: string;
  state: RunState;
  ngspice?: string;
  deckSha?: string;
  started?: string;
  finished?: string;
  tookMs: number;
  error?: string;
  problems: string[];
  measures: RunMeasure[];
  plots: RunPlot[];
}

/** A target checked against a variation's latest run in a condition; pass is absent without a value. */
export interface CheckResult extends SimCheck {
  variation: string;
  condition: string;
  value?: number;
  pass?: boolean;
}

/** A variation with its analyses, each condition's latest run and its targets. */
export interface VariationDetail {
  variation: Variation;
  analyses: { kind: string; line: string }[];
  measures: string[];
  latest: SimRun[];
  checks: CheckResult[];
}

/** A test with its variations, in the order they were added. */
export interface SimulationDetail {
  simulation: Simulation;
  variations: VariationDetail[];
  runs: SimRun[];
}

/** A test in a project's list. */
export interface SimulationSummary {
  name: string;
  kind?: string;
  purpose?: string;
  target?: string;
  updated: string;
  variations: number;
  /** The latest variation's name. */
  latest?: string;
  /** The latest variation's state by condition. */
  states: Record<string, RunState>;
  pass: number;
  fail: number;
  presented: boolean;
}

/** One line of a chart. */
export interface SimSeries {
  name: string;
  unit: string;
  x: number[];
  y: number[];
}

/** A run's chart data: series against the plot's scale. */
export interface SimulationData {
  run: number;
  variation: string;
  condition: string;
  plot: string;
  xName: string;
  xUnit: string;
  series: SimSeries[];
}

/** A point, in millimetres. */
export interface CadPt {
  x: number;
  y: number;
}

/** A shape of a CAD drawing, coloured by its role (copper, silk, body…). */
export interface CadShape {
  kind: 'line' | 'rect' | 'poly' | 'circle' | 'text';
  role: string;
  pts?: CadPt[];
  /** A circle's radius or a rect's corner radius. */
  r?: number;
  width?: number;
  text?: string;
  size?: number;
  anchor?: 'start' | 'middle' | 'end';
  vertical?: boolean;
  /** A 3D face's shaded colour. */
  color?: string;
}

/** A symbol, footprint or 3D model as shapes, y down. */
export interface CadDrawing {
  view: { x0: number; y0: number; x1: number; y1: number };
  items: CadShape[];
}

export interface CadProblem {
  level: 'error' | 'warning';
  text: string;
}

/** Where a symbol or footprint came from: SheetDiver, an agent, or the user's edit in their CAD tool. */
export type CadState = 'auto' | 'drawn' | 'edited';

export interface CadSymbolPin {
  number: string;
  name: string;
  type: string;
  side: string;
}

export interface CadPad {
  number: string;
  kind: string;
}

export interface CadDim {
  min?: number;
  nom?: number;
  max?: number;
}

/** A package as its datasheet drawing gives it. */
export interface CadOutline {
  family: string;
  name?: string;
  pins: number;
  pitch?: number;
  bodyLength?: CadDim;
  bodyWidth?: CadDim;
  height?: CadDim;
  span?: CadDim;
  leadLength?: CadDim;
  leadWidth?: CadDim;
  exposedPad?: { w: number; h: number };
  land?: { padLength: number; padWidth: number; rowSpan: number; page?: number };
  page?: number;
}

/** A symbol or footprint as the app shows it. */
export interface CadView {
  name: string;
  state: CadState;
  source?: string;
  notes?: string;
  updated?: string;
  problems?: CadProblem[];
  drawing: CadDrawing;
  symbol?: { name: string; pins: CadSymbolPin[] };
  footprint?: { name: string; description: string; from: string; pads: CadPad[] };
  package?: CadOutline;
  model?: CadDrawing;
  standard?: boolean;
  modelKept?: boolean;
}

/** A part's symbol and footprint for one of its packages. */
export interface CadPackage {
  package: string;
  pins: number;
  symbol?: CadView;
  footprint?: CadView;
  match?: CadProblem[];
  missing?: string;
}

export interface CadFormatInfo {
  name: string;
  label: string;
  readsEdits: boolean;
}

/** Where and how the user's CAD library is kept. */
export interface CadLibrary {
  dir: string;
  name: string;
  formats: string[] | null;
  available: CadFormatInfo[];
  kicad: { dir: string; symbols: string; footprints: string; models: string };
  eagle: string;
}

export interface PartCad {
  part: Part;
  packages: CadPackage[];
  library: CadLibrary;
}

/** What a sync of the CAD library did. */
export interface CadReport {
  dir: string;
  written: Record<string, string>;
  adopted?: string[];
  symbols: number;
  footprints: number;
}
