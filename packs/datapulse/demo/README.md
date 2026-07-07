# datapulse demo asset — «Аудит НСИ»

Sample dirty counterparty directory + a driver script that runs the real
datapulse pipeline (ConnectorAgent ingest -> MatchAgent exact-match dedup ->
Steward review queue) end to end against a live kalita node.

## Files

- `nsi_directory.csv` / `nsi_directory.xlsx` — the SAME 26-row dataset in both
  shapes `workers/connectors/excel_ingest.py` accepts. Columns: `name`, `inn`,
  `city`, `address` (kept as plain ASCII header names so the CLI/query
  `--key-columns`/`--primary-column` arguments never risk a Windows console
  encoding surprise; the DATA values are realistic Russian counterparty
  records — `ООО`/`АО`/`ПАО`, ИНН, город, адрес). The `.xlsx` was generated
  with stdlib `zipfile` + `xml.etree.ElementTree` only (mirrors how
  `excel_ingest.py` itself reads xlsx, and how its own test fixtures are
  built) — no openpyxl/pandas anywhere in this repo.
- `run_demo.py` — the driver (see below).

`run_demo.py` ingests **only the `.csv`** by default. The `.xlsx` is provided
as an equivalent alternate-format copy so the connector's xlsx path can be
exercised directly (`python excel_ingest.py nsi_directory.xlsx --primary-column name`)
or handed to a variant of the driver. Ingesting *both* files in the same run
would create a second Source with the identical 26 rows and — being byte-
identical content — would itself produce three more "duplicate" MatchGroups
purely from having the same file twice under two names. That is real,
expected exact_match behavior (cross-source dedup is its deliberate default),
just not part of the curated boundary story below, so the scripted report
keeps to one ingest to keep the cluster narrative legible.

## The intended duplicate clusters

`exact_match.py`'s level-1 matcher normalizes each configured key column with
exactly: **trim** leading/trailing whitespace, **collapse** internal
whitespace runs to one space, **case-fold** — and deliberately nothing else
(no punctuation/quote stripping, no accent folding, no numeric coercion). The
driver runs it with `--key-columns name,inn`. Rows are numbered by their
1-based position in the data (header excluded), matching both files' row
order.

### Clusters that SHOULD collapse into one MatchGroup

| Rows | Company (as written) | Why they collapse |
|---|---|---|
| 1–3 | `ООО "Ромашка"` / `ооо   "ромашка"` / `  ООО "Ромашка"  ` (same ИНН 7701234567 in all three) | Differ only by case, extra internal whitespace, and leading/trailing whitespace — trim + whitespace-collapse + case-fold make all three normalize to the identical string `ооо "ромашка"`; ИНН is written identically in all three so its normalized form matches too. -> **one MatchGroup of 3.** |
| 4–5 | `АО "Стройинвест"` / `АО   "Стройинвест"` (ИНН 7802345678 both) | Differ only by doubled internal spaces (name and address) — whitespace-collapse alone equalizes them. -> **one MatchGroup of 2.** |
| 6–7 | `ПАО "Северсталь"` / `пао "северсталь"` (ИНН 3525037972 both) | Differ only by case (name AND city/address, though city isn't a key column so it's irrelevant to the match) — case-fold equalizes the key. -> **one MatchGroup of 2.** |

### Near-duplicates deliberately left unmatched at level-1

| Rows | Company (as written) | Why level-1 correctly declines |
|---|---|---|
| 8–9 | `ООО "Технопарк"` vs `О.О.О. Технопарк` (same ИНН 7703456789, same city/address) | Same real company and same ИНН, but the **punctuation** differs (`О.О.О.` with dots vs `ООО`, and the quote marks around the name are dropped) — level-1's normalization explicitly does NOT strip punctuation, so the normalized name strings differ (`ооо "технопарк"` vs `о.о.о. технопарк`) and the key tuple does not match. This is the matcher's honest boundary, not a bug: fixing it is fuzzy/edit-distance matching, out of scope for level-1 by the module's own docstring. |
| 10–11 | `АО "Стройсервис"` / `АО "Стройсервис"` with ИНН `5029087654` vs `5029087653` (typo in the last digit) | Name normalizes identically, but the ИНН differs by one digit — a real (if small) data-entry error, not a formatting difference. Since the key is `(name, inn)`, one differing component is enough to keep the key tuple, and hence the group, apart. Level-1 does no numeric/edit-distance tolerance — that is exactly what it is *not* supposed to do. |

### Singles (no intended duplicate)

The remaining 15 rows are distinct companies (distinct name+ИНН pairs) across
various Russian cities — filler volume so the demo reads like a real, mixed
directory rather than a hand-picked puzzle, and to prove the matcher leaves
genuinely unique records alone.

### Expected totals

- 26 MasterRecords ingested from one Source.
- 3 MatchGroups proposed (sizes 3, 2, 2) — 7 records flagged.
- 19 records with no exact-match duplicate under level-1 (15 true singles +
  the 4 rows in the two "should NOT match" pairs above, each of which is
  unique on the full `(name, inn)` key even though it has a near-duplicate
  sibling a human should still eyeball).

## Running it

The driver needs a live node and one bearer token per identity it drives.
Tokens are read from the environment only — never hardcoded — because a
`--demo` node mints fresh ones on every start (see `run_demo.py`'s header).

```
# 1. stand up an in-memory demo node with the datapulse pack (no Postgres needed)
kalita serve --pack packs/datapulse --demo --listen 127.0.0.1:8090

# 2. from the node's startup log, export the three tokens it printed
export DATAPULSE_CONNECTOR_TOKEN=<demo-agent-ConnectorAgent token>
export DATAPULSE_MATCH_TOKEN=<demo-agent-MatchAgent token>
export DATAPULSE_STEWARD_TOKEN=<demo-Steward token>

# 3. run the audit (KALITA_URL defaults to http://127.0.0.1:8090)
python packs/datapulse/demo/run_demo.py
#   or exercise the xlsx path:  run_demo.py --file nsi_directory.xlsx
```

The node is in-memory, so its journal (and these tokens) vanish on exit; the
connectors' uuid5 idempotency keys make the driver safe to re-run against a
still-running node — it converges to the same Source/MasterRecords/MatchGroups
rather than piling up duplicates.
