-- Lookup table for appraisal statuses (FK target instead of a bare TEXT).
-- Two states only: an appraisal is in progress, then completed.
CREATE TABLE appraisal_statuses (
    id         TEXT PRIMARY KEY,
    sort_order INTEGER NOT NULL
);

INSERT INTO appraisal_statuses (id, sort_order) VALUES
    ('in_progress', 1),
    ('completed', 2);

-- One appraisal per request (request_id UNIQUE). appraiser_id is nullable: the
-- row is created unassigned when the request enters the appraisal status, and an
-- appraiser takes it later. market_value is entered manually — the calculation
-- engine is out of scope until the formulas are formalized (risk R-001).
-- report_s3_key: the report binary lives in S3; we store only the object key.
CREATE TABLE appraisals (
    id            UUID PRIMARY KEY,
    request_id    UUID NOT NULL UNIQUE,
    appraiser_id  UUID,
    status        TEXT NOT NULL DEFAULT 'in_progress' REFERENCES appraisal_statuses(id),
    notes         TEXT,
    market_value  NUMERIC(15, 2),
    report_s3_key TEXT,
    completed_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_appraisals_appraiser_id ON appraisals (appraiser_id);
CREATE INDEX idx_appraisals_status ON appraisals (status);

-- Comparable (analog) objects the appraiser bases the evaluation on. Field set
-- is not formalized yet, so everything lives in free-form JSONB. ON DELETE
-- CASCADE so comparables disappear with their appraisal.
CREATE TABLE comparables (
    id           UUID PRIMARY KEY,
    appraisal_id UUID NOT NULL REFERENCES appraisals(id) ON DELETE CASCADE,
    data         JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_comparables_appraisal_id ON comparables (appraisal_id);
