-- migrate:up

-- Drop the logs table (the lifecycle increment of the unified-task-event-stream
-- proposal). Every log row now has an event-type home in the stream: `llm` ->
-- report events (already cut over), `audit`/`info` -> lifecycle events,
-- `error` -> the lifecycle payload's message field, and `mcp` breadcrumbs are
-- dropped outright (echoes of link/instruction events). There is no separate
-- verbose channel, so the table goes away entirely with no backfill.
DROP TABLE logs;

-- migrate:down

CREATE TABLE public.logs (
    id bigint NOT NULL,
    task_id bigint NOT NULL,
    type text NOT NULL,
    content text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE SEQUENCE public.logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.logs_id_seq OWNED BY public.logs.id;

ALTER TABLE ONLY public.logs ALTER COLUMN id SET DEFAULT nextval('public.logs_id_seq'::regclass);

ALTER TABLE ONLY public.logs
    ADD CONSTRAINT logs_pkey PRIMARY KEY (id);

CREATE INDEX idx_logs_task_id ON public.logs USING btree (task_id);

ALTER TABLE ONLY public.logs
    ADD CONSTRAINT logs_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE CASCADE;
