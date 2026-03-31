ALTER TABLE verification_events ADD COLUMN refinement_id INTEGER REFERENCES refinements(id);

CREATE INDEX IF NOT EXISTS idx_verification_refinement ON verification_events(refinement_id);
