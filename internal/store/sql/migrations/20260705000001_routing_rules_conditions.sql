-- migrate:up
-- Before v2.9.0, org routing rules were stored in the legacy flat-matcher shape
-- with top-level matcher fields (prefix/mention/assignee/url_prefix/value).
-- v2.9.0 read those rows by translating them to attribute conditions on the fly
-- ("translate-on-read"). That path is being removed, so any legacy row still in
-- orgs.routing_rules must be normalized to the conditions-native shape now:
-- otherwise a plain json.Unmarshal would silently drop the matcher fields
-- (encoding/json ignores unknown keys), turning a targeted rule into one that
-- matches every event of its type.
--
-- Each legacy matcher field maps to exactly one condition, mirroring the removed
-- eventrouter.TranslateRule:
--   prefix     -> {attr:"body",     op:"prefix", value:<prefix>}
--   mention    -> {attr:"mention",  op:"equals", value:<mention>}
--   assignee   -> {attr:"assignee", op:"equals", value:<assignee>}
--   url_prefix -> {attr:"url",      op:"prefix", value:<url_prefix>}
--   value      -> {attr:"label",    op:"equals", value:<value>}
--
-- The v1 translator fanned a type-less rule out to one concrete rule per
-- registered event type (dropping conditions on attrs a type does not emit). A
-- per-row rewrite that leaves Source/Type as-is is behaviorally equivalent under
-- the matcher: empty Source/Type are wildcards, and a condition on an attr the
-- event does not carry simply fails to match — exactly like a fanned-out rule
-- that never applied to that type. So this rewrite preserves matching without
-- needing the Go schema registry.
--
-- Idempotent: rows with no legacy matcher fields are skipped, and once rewritten
-- the fields are gone so a re-run is a no-op.
UPDATE orgs
SET routing_rules = (
    SELECT jsonb_agg(
        (elem - ARRAY['prefix', 'mention', 'assignee', 'url_prefix', 'value'])
        || CASE
             WHEN jsonb_array_length(conds) > 0 THEN jsonb_build_object('conditions', conds)
             ELSE '{}'::jsonb
           END
        ORDER BY idx
    )
    FROM jsonb_array_elements(routing_rules) WITH ORDINALITY AS t(elem, idx),
    LATERAL (
        SELECT COALESCE(elem -> 'conditions', '[]'::jsonb)
            || CASE WHEN COALESCE(elem ->> 'prefix', '')     <> '' THEN jsonb_build_array(jsonb_build_object('attr', 'body',     'op', 'prefix', 'value', elem ->> 'prefix'))     ELSE '[]'::jsonb END
            || CASE WHEN COALESCE(elem ->> 'mention', '')    <> '' THEN jsonb_build_array(jsonb_build_object('attr', 'mention',  'op', 'equals', 'value', elem ->> 'mention'))    ELSE '[]'::jsonb END
            || CASE WHEN COALESCE(elem ->> 'assignee', '')   <> '' THEN jsonb_build_array(jsonb_build_object('attr', 'assignee', 'op', 'equals', 'value', elem ->> 'assignee'))   ELSE '[]'::jsonb END
            || CASE WHEN COALESCE(elem ->> 'url_prefix', '') <> '' THEN jsonb_build_array(jsonb_build_object('attr', 'url',      'op', 'prefix', 'value', elem ->> 'url_prefix')) ELSE '[]'::jsonb END
            || CASE WHEN COALESCE(elem ->> 'value', '')      <> '' THEN jsonb_build_array(jsonb_build_object('attr', 'label',    'op', 'equals', 'value', elem ->> 'value'))      ELSE '[]'::jsonb END
        AS conds
    ) AS c
)
WHERE EXISTS (
    SELECT 1
    FROM jsonb_array_elements(routing_rules) AS elem
    WHERE elem ? 'prefix'
       OR elem ? 'mention'
       OR elem ? 'assignee'
       OR elem ? 'url_prefix'
       OR elem ? 'value'
);

-- migrate:down
-- Irreversible: this is a one-way normalization to the conditions shape (the code
-- that read the legacy shape is gone), so there is nothing to restore.
SELECT 1;
