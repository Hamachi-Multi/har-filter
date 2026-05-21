import { emptyBodyPreview } from "./body-preview.js";
import { loadSettings } from "./settings.js";

const savedSettings = loadSettings();

export const state = {
  sessionId: "",
  fileName: "",
  entries: [],
  entriesVersion: 0,
  filterCache: {
    version: -1,
    key: "",
    entries: []
  },
  selected: new Set(),
  filter: "",
  resourceTypeFilter: savedSettings.resourceTypeFilter,
  page: 1,
  pageSize: savedSettings.pageSize,
  activeEntryIndex: null,
  focusEntryIndex: null,
  detailRequestId: 0,
  entryDialogRequestId: 0,
  entryDialogIndex: null,
  bodyDialogKind: "",
  bodyDialogMode: "pretty",
  bodyDialogTrigger: null,
  entryDialogTrigger: null,
  bodyPreviews: {
    request: emptyBodyPreview("Request body"),
    response: emptyBodyPreview("Response body"),
    entryRequest: emptyBodyPreview("Request body"),
    entryResponse: emptyBodyPreview("Response body")
  },
  uploadFile: null,
  fileDragDepth: 0,
  busy: false
};

export const dragSelection = {
  active: false,
  dragging: false,
  targetChecked: false,
  touched: new Set(),
  pointerId: null,
  startX: 0,
  startY: 0,
  startRow: null,
  suppressClick: false
};

export const entryClickTiming = {
  index: null,
  timeStamp: 0,
  suppressDoubleClickUntil: 0
};

export const entrySearchTextCache = new WeakMap();
export const copySuccessTimers = new WeakMap();
export const bodyPrettyCache = new Map();
