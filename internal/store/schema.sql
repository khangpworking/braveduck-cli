-- braveduck-cli v1 schema. Port of the battle-tested job_listings shape from the
-- original personal-assistant app (see ARCHITECTURE.md §2.3 and functional-spec
-- §11), adapted to plain SQLite. Timestamps are TEXT (ISO-8601) throughout, as in
-- the source. Applied idempotently at Open via CREATE TABLE IF NOT EXISTS.

CREATE TABLE IF NOT EXISTS jobs (
  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
  title                 TEXT NOT NULL,
  company               TEXT NOT NULL DEFAULT '',
  url                   TEXT,
  location              TEXT,
  salary                TEXT,
  skills                TEXT,                          -- comma-separated
  status                TEXT NOT NULL DEFAULT 'saved', -- saved/waiting/applied/interview/offer/closed
  pinned                INTEGER NOT NULL DEFAULT 0,
  keep                  INTEGER NOT NULL DEFAULT 0,
  notes                 TEXT,

  jd_summary            TEXT,
  jd_ai_summary         TEXT,
  jd_full               TEXT,

  discovery_source      TEXT,
  saved_at              TEXT NOT NULL,

  score                 REAL,
  score_letter          TEXT,
  score_status          TEXT,                          -- pending/scored/failed
  score_reasoning       TEXT,

  apply_method          TEXT,                          -- email/platform
  apply_state           TEXT,                          -- queued/filling/cooldown/failed/applied
  apply_next_at         TEXT,
  apply_detail          TEXT,
  apply_updated_at      TEXT,

  cv_path               TEXT,
  cv_status             TEXT,
  cv_version            INTEGER,
  cv_generated_at       TEXT,

  draft_email           TEXT,
  draft_status          TEXT,
  draft_status_at       TEXT,
  draft_notes           TEXT,
  contact_email         TEXT,

  expires_at            TEXT,                          -- ''/unknown/expire:ISO/applied:ISO
  followup_sent_at      TEXT,

  topcv_status          TEXT,
  vnw_status            TEXT,

  company_active_date   TEXT,
  company_distance_km   REAL,
  company_geo_status    TEXT,
  company_masothue_url  TEXT,
  company_masothue_name TEXT,

  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_source ON jobs(discovery_source);

CREATE TABLE IF NOT EXISTS job_ratings (   -- per-model A/B ratings, one row per (job, model)
  job_id          INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  model           TEXT NOT NULL,
  status          TEXT NOT NULL,           -- pending/scored/failed
  score           REAL,
  score_letter    TEXT,
  score_reasoning TEXT,
  PRIMARY KEY (job_id, model)
);

CREATE TABLE IF NOT EXISTS platform_sessions (  -- encrypted session blobs, one per platform
  platform     TEXT PRIMARY KEY,          -- topcv/vietnamworks/joboko/careerlink
  session_enc  BLOB NOT NULL,
  created_at   TEXT NOT NULL,
  expires_hint TEXT
);

CREATE TABLE IF NOT EXISTS crawl_runs (
  name              TEXT PRIMARY KEY,
  last_success_date TEXT,
  last_run_at       TEXT
);

CREATE TABLE IF NOT EXISTS keywords (      -- crawler keyword lists, per source
  source  TEXT NOT NULL,                   -- topcv/vietnamworks/serpapi/...
  keyword TEXT NOT NULL,
  kind    TEXT NOT NULL DEFAULT 'include', -- include/exclude (negative keywords)
  PRIMARY KEY (source, keyword, kind)
);

CREATE TABLE IF NOT EXISTS rating_config (  -- single-row A/B rater config
  id            INTEGER PRIMARY KEY CHECK (id = 1),
  ab_enabled    INTEGER NOT NULL DEFAULT 0, -- 0/1: run challengers or just the default
  models        TEXT NOT NULL DEFAULT '',   -- comma-separated selected rater ids (1..3)
  default_model TEXT NOT NULL DEFAULT ''     -- ∈ models; owns the board badge
);
