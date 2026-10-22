# Search (boards f-open, f-results, f-empty) — PROPOSED, not in the first release

The canvas marks search as *Proposed. Not in the first release.* Build after M6 is otherwise done; the server side is FTS (B3 "FTS finish"). Story: none yet (PLAN gap).

## Open (f-open)
Full-screen modal from a header action (proposal: long-press the "October 2026" title or a search glyph left of the Today button; DECISIONS Q5 is the same unresolved header-entry question). Layout: nav row (h56) with a close glyph (44) and a **search field in place of the title**: `in f` h44 (1pt `ink` border, `surface`), placeholder **"Search events and people"** dim, flex 1, margin 0/8. Label **"Recent"** + rows (`.r`, 17pt: "Sam", "Design review", "Dentist", tap fills the field; swipe left removes). Label **"Filter"** + chips (`ChipToggle`): Today · This week · Reply needed · Has a call. Hint 13 dim: "Search looks across all connected accounts, on your server." (the "Proposed" sentence is an annotation, not UI).

## Results (f-results)
Query "sam": label **"Coming up · 3"**, rows `.r.r2` (h72) with account bar, a 56pt mono time column holding the **date** ("Today", "Fri 16", "Thu 22"), title 19, sub 14 dim "12:30 · Kōya" (time + place); label **"Past · 2"** with the same rows at **45% opacity** ("Oct 2", "Sep 28"). Sort: coming up ascending, past descending. Results update as you type (250 ms debounce); matching text in titles is not highlighted (no colour fills; optional 1pt underline in `ink`). Tap → Event detail. People matches show as a first group **"People"** (initials box rows) only when ≥ 1.

## Nothing found (f-empty)
`QuietCard`: **"Nothing found"** / dim `No events or people match "samuel".`; outline button **"Ask alfather instead"** (margin-top 24) opening Find free time-style ask sheet with the query. Offline: search runs on the local saved agenda only and the sub line says "Searching saved events only."

## Components
★`SearchField` (nav-embedded field); reuse `ListRow`, `ChipToggle`, `QuietCard`, `CalButton`.

## Done means
Open, results, nothing-found in dark and light match f-* boards.
