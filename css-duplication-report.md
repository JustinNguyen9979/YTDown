# CSS Duplication Report - style.css

**File analyzed:** `/frontend/style.css`
**Total lines:** 2230
**Date:** 2026-05-19

---

## DUPLICATIONS

### 1. SCROLLBAR_STYLES
**Severity:** MEDIUM
**Occurrences:** 4
**Lines:** 1380-1396, 1656-1671, 1830-1845, 1856-1861
**Selectors:**
  - `::-webkit-scrollbar`
  - `#info.tab-content.active::-webkit-scrollbar`
  - `.error-log-container::-webkit-scrollbar`
  - `.batch-table tbody::-webkit-scrollbar`
**Issue:** All share identical structure: `width`, `track { background: transparent }`, `thumb { border-radius, background-color }`, `thumb:hover { background-color }`. Only differs in width (5px vs 8px) and thumb color opacity.
**Recommendation:** Extract to CSS variables or a single base rule with overrides.

---

### 2. STATUS_ERROR_DUPLICATE
**Severity:** MEDIUM
**Occurrences:** 2
**Lines:** 1359-1361, 1806-1810
**Selectors:**
  - `.batch-table .status-error`
  - `.status-error`
**Issue:** Both define `color: var(--accent-red)`. Second definition adds `font-weight: 600` and `animation: error-pulse`.
**Recommendation:** Merge into single `.status-error` rule.

---

### 3. PROGRESS_BAR_DUPLICATE
**Severity:** HIGH
**Occurrences:** 2 pairs (4 selectors)
**Lines:** 979-986 vs 1014-1020, 988-997 vs 1022-1031
**Selectors:**
  - `.progress-bar` vs `.batch-progress-bar`
  - `.progress-fill` vs `.batch-progress-fill`
**Issue:** Nearly identical properties. Only `height` differs (7px vs 5px) and `box-shadow` intensity.
**Shared properties:**
  - `width: 100%`
  - `background: rgba(255, 255, 255, 0.07)`
  - `border-radius: 99px`
  - `overflow: hidden`
  - `animation: shimmer-flow 1.8s linear infinite`
  - `transition: width 0.4s cubic-bezier(0.4, 0, 0.2, 1)`
**Recommendation:** Use shared base classes with modifier classes for height/shadow variations.

---

### 4. FOCUS_STYLES_DUPLICATE
**Severity:** MEDIUM
**Occurrences:** 3
**Lines:** 442-446, 641-645, 850-854
**Selectors:**
  - `.url-input:focus, .batch-input:focus`
  - `select:focus`
  - `.cookie-inline-input:focus`
**Issue:** Identical focus styles repeated 3 times.
**Shared properties:**
  ```css
  outline: none;
  border-color: var(--accent-blue);
  box-shadow: 0 0 0 2px rgb(10, 132, 255, 0.2);
  ```
**Recommendation:** Create a shared `.focus-ring` utility class or group selectors.

---

### 5. BACKDROP_FILTER_DUPLICATE
**Severity:** LOW
**Occurrences:** 3
**Lines:** 436-437, 632-633, 1443-1444
**Selectors:**
  - `.url-input, .batch-input`
  - `select`
  - `.custom-select-trigger`
**Issue:** Same backdrop-filter declaration repeated.
**Shared properties:**
  ```css
  backdrop-filter: blur(20px) saturate(180%);
  -webkit-backdrop-filter: blur(20px) saturate(180%);
  ```
**Recommendation:** Extract to CSS variable or utility class.

---

### 6. OPTION_ROW_CONFLICT
**Severity:** MEDIUM
**Occurrences:** 2
**Lines:** 510-513, 1618-1625
**Selectors:**
  - `#batch .option-row`
  - `.option-row`
**Issue:** Same class name with different properties in different scopes. May cause unexpected behavior.
**Properties (scoped):**
  - `#batch .option-row`: `flex: 0 0 100%; margin-top: 10px;`
  - `.option-row`: `display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text-primary); cursor: pointer;`
**Recommendation:** Rename one of the classes to avoid collision (e.g., `.batch-option-row`).

---

### 7. MARGIN_PADDING_RESET_REDUNDANT
**Severity:** LOW
**Occurrences:** 2
**Lines:** 1-5, 32-34
**Selectors:**
  - `*`
  - `html, body`
**Issue:** `*` selector already resets margin/padding for all elements including html/body.
**Recommendation:** Remove margin/padding from `html, body` block.

---

### 8. BOX_SIZING_REDUNDANT
**Severity:** LOW
**Occurrences:** 2
**Lines:** 4, 45
**Selectors:**
  - `*`
  - `.container`
**Issue:** `*` selector already sets `box-sizing: border-box` globally.
**Recommendation:** Remove from `.container`.

---

### 9. FADE_ANIMATION_VARIANTS
**Severity:** LOW
**Occurrences:** 3
**Lines:** 59-62, 408-417, 1491-1494
**Animations:**
  - `fadeInDown`: `translateY(-6px) -> 0`
  - `fadeIn`: `translateY(10px) -> 0`
  - `slideDown`: `translateY(-10px) -> 0`
**Issue:** All share `opacity: 0 -> 1` pattern with only translateY direction/distance differing.
**Recommendation:** Consider single animation with CSS variable for translateY value.

---

### 10. DROPDOWN_ITEM_STYLES_DUPLICATE
**Severity:** MEDIUM
**Occurrences:** 3 pairs (6 selectors)
**Lines:** 1508-1511 vs 891-894, 1513-1517 vs 896-900, 1519-1522 vs 902-905
**Selectors:**
  - `.custom-option:hover` vs `.cookie-dropdown-item:hover`
  - `.custom-option.selected` vs `.cookie-dropdown-item.selected`
  - `.custom-option.selected:hover` vs `.cookie-dropdown-item.selected:hover`
**Issue:** Identical hover/selected styles for two different dropdown implementations.
**Shared properties:**
  - `:hover` -> `background: var(--accent-blue); color: white;`
  - `.selected` -> `color: var(--accent-blue); font-weight: 600; background: rgba(10, 132, 255, 0.1);`
  - `.selected:hover` -> `color: white; background: var(--accent-blue);`
**Recommendation:** Create shared `.dropdown-item` utility classes.

---

### 11. BATCH_TABLE_TH_BACKGROUND_DUPLICATE
**Severity:** LOW
**Occurrences:** 2
**Lines:** 1283-1290, 2222-2230
**Selectors:**
  - `.batch-table th`
  - `.batch-table thead th`
**Issue:** Both define `background: var(--bg-primary)` for table headers.
**Recommendation:** Merge into single rule.

---

### 12. TOOLTIP_PATTERN_DUPLICATE
**Severity:** LOW
**Occurrences:** 2
**Lines:** 328-351, 940-959
**Selectors:**
  - `.tooltip-text`
  - `.cookie-warning-tooltip`
**Issue:** Both share similar tooltip styling pattern.
**Shared properties:**
  - `position: absolute`
  - `border-radius: 8px`
  - `background: rgba(dark)`
  - `border: 1px solid var(--border-color)`
  - `opacity: 0`
  - `pointer-events: none`
  - `transform: translateY(-4px)`
  - `transition: opacity + transform`
  - High `z-index`
**Recommendation:** Extract to base `.tooltip` class.

---

### 13. HIDDEN_ATTRIBUTE_DUPLICATE
**Severity:** LOW
**Occurrences:** 2
**Lines:** 1817, 1833
**Selectors:**
  - `.cookie-inline[hidden]`
  - `.cookie-added-badge[hidden]`
**Issue:** Both set `display: none !important` for `[hidden]` attribute.
**Recommendation:** Use single `[hidden] { display: none !important; }` rule.

---

### 14. THEAD_STICKY_DUPLICATE
**Severity:** LOW
**Occurrences:** 2
**Lines:** 1097-1101, 2222-2230
**Selectors:**
  - `.batch-table thead`
  - `.batch-table thead th`
**Issue:** Both define `position: sticky; top: 0;` with different z-index values (2 vs 3).
**Recommendation:** Consolidate into single rule with consistent z-index.

---

### 15. TABLE_COLUMN_WIDTHS_PATTERN
**Severity:** LOW
**Occurrences:** 2 tables grouped
**Lines:** 1159-1181
**Selectors:**
  - `#galleryTable th:nth-child(n), #galleryTable td:nth-child(n)`
  - `#compressTable th:nth-child(n), #compressTable td:nth-child(n)`
**Issue:** Already well-grouped, but will require copy-paste if new tables are added.
**Column widths:** 35px, auto, 130px, 120px
**Recommendation:** Consider CSS custom properties for column widths if more tables are added.

---

## SUMMARY

| ID | Category | Severity | Occurrences | Lines |
|----|----------|----------|-------------|-------|
| 1 | SCROLLBAR_STYLES | MEDIUM | 4 | 1380-1396, 1656-1671, 1830-1845, 1856-1861 |
| 2 | STATUS_ERROR_DUPLICATE | MEDIUM | 2 | 1359-1361, 1806-1810 |
| 3 | PROGRESS_BAR_DUPLICATE | HIGH | 4 | 979-986, 1014-1020, 988-997, 1022-1031 |
| 4 | FOCUS_STYLES_DUPLICATE | MEDIUM | 3 | 442-446, 641-645, 850-854 |
| 5 | BACKDROP_FILTER_DUPLICATE | LOW | 3 | 436-437, 632-633, 1443-1444 |
| 6 | OPTION_ROW_CONFLICT | MEDIUM | 2 | 510-513, 1618-1625 |
| 7 | MARGIN_PADDING_RESET_REDUNDANT | LOW | 2 | 1-5, 32-34 |
| 8 | BOX_SIZING_REDUNDANT | LOW | 2 | 4, 45 |
| 9 | FADE_ANIMATION_VARIANTS | LOW | 3 | 59-62, 408-417, 1491-1494 |
| 10 | DROPDOWN_ITEM_STYLES_DUPLICATE | MEDIUM | 6 | 891-905, 1508-1522 |
| 11 | BATCH_TABLE_TH_BACKGROUND_DUPLICATE | LOW | 2 | 1283-1290, 2222-2230 |
| 12 | TOOLTIP_PATTERN_DUPLICATE | LOW | 2 | 328-351, 940-959 |
| 13 | HIDDEN_ATTRIBUTE_DUPLICATE | LOW | 2 | 1817, 1833 |
| 14 | THEAD_STICKY_DUPLICATE | LOW | 2 | 1097-1101, 2222-2230 |
| 15 | TABLE_COLUMN_WIDTHS_PATTERN | LOW | 2 | 1159-1181 |

**Total duplications found:** 15
**High severity:** 1
**Medium severity:** 5
**Low severity:** 9
