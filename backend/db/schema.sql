
-- Dumped from database version 17.11
-- Dumped by pg_dump version 18.4

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: btree_gist; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS btree_gist WITH SCHEMA public;


--
-- Name: EXTENSION btree_gist; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION btree_gist IS 'support for indexing common datatypes in GiST';


--
-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;


--
-- Name: EXTENSION pgcrypto; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION pgcrypto IS 'cryptographic functions';


--
-- Name: app_key_kind; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.app_key_kind AS ENUM (
    'sdk',
    'coordinator'
);


--
-- Name: app_platform; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.app_platform AS ENUM (
    'ios',
    'android',
    'flutter',
    'react_native'
);


--
-- Name: tracker; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.tracker AS ENUM (
    'github',
    'gitlab',
    'bitbucket'
);


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: app_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.app_keys (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    app_id uuid NOT NULL,
    key_hash bytea NOT NULL,
    key_prefix text NOT NULL,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    kind public.app_key_kind NOT NULL
);


--
-- Name: app_projects; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.app_projects (
    app_id uuid NOT NULL,
    project_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: app_tracker_credentials; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.app_tracker_credentials (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    app_id uuid NOT NULL,
    tracker public.tracker NOT NULL,
    client_id text NOT NULL,
    client_secret_ciphertext bytea NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT app_tracker_credentials_tracker_check CHECK ((tracker = ANY (ARRAY['gitlab'::public.tracker, 'bitbucket'::public.tracker])))
);


--
-- Name: apps; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.apps (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    owner_id uuid NOT NULL,
    name text NOT NULL,
    platform public.app_platform NOT NULL,
    bundle_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: connection_secrets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.connection_secrets (
    provider_connection_id uuid NOT NULL,
    ciphertext bytea NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: issues; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.issues (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_repository_id uuid NOT NULL,
    app_id uuid,
    external_id text NOT NULL,
    url text NOT NULL,
    title text NOT NULL,
    state text DEFAULT 'open'::text NOT NULL,
    fingerprint text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    provider_connection_id uuid,
    CONSTRAINT issues_state_check CHECK ((state = ANY (ARRAY['open'::text, 'closed'::text])))
);


--
-- Name: owners; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.owners (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    name text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: project_repositories; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.project_repositories (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    repository text NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    tracker public.tracker NOT NULL
);


--
-- Name: projects; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.projects (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    owner_id uuid NOT NULL
);


--
-- Name: provider_connections; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_connections (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    owner_id uuid NOT NULL,
    tracker public.tracker NOT NULL,
    external_id text NOT NULL,
    account_login text NOT NULL,
    account_type text NOT NULL,
    suspended_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    app_id uuid NOT NULL
);


--
-- Name: report_attachments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.report_attachments (
    report_id uuid NOT NULL,
    name text NOT NULL,
    content_type text NOT NULL,
    data bytea NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: report_deliveries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.report_deliveries (
    report_id uuid NOT NULL,
    project_repository_id uuid NOT NULL,
    issue_id uuid,
    url text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: reports; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.reports (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    app_id uuid NOT NULL,
    report_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    session_id text NOT NULL,
    trigger text NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    queued_at timestamp with time zone,
    payload jsonb NOT NULL,
    processed_at timestamp with time zone
);


--
-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.schema_migrations (
    version character varying NOT NULL
);


--
-- Name: app_keys app_keys_key_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_keys
    ADD CONSTRAINT app_keys_key_hash_key UNIQUE (key_hash);


--
-- Name: app_keys app_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_keys
    ADD CONSTRAINT app_keys_pkey PRIMARY KEY (id);


--
-- Name: app_projects app_projects_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_projects
    ADD CONSTRAINT app_projects_pkey PRIMARY KEY (app_id, project_id);


--
-- Name: app_tracker_credentials app_tracker_credentials_app_id_tracker_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_tracker_credentials
    ADD CONSTRAINT app_tracker_credentials_app_id_tracker_key UNIQUE (app_id, tracker);


--
-- Name: app_tracker_credentials app_tracker_credentials_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_tracker_credentials
    ADD CONSTRAINT app_tracker_credentials_pkey PRIMARY KEY (id);


--
-- Name: apps apps_id_owner_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.apps
    ADD CONSTRAINT apps_id_owner_id_key UNIQUE (id, owner_id);


--
-- Name: apps apps_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.apps
    ADD CONSTRAINT apps_pkey PRIMARY KEY (id);


--
-- Name: connection_secrets connection_secrets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connection_secrets
    ADD CONSTRAINT connection_secrets_pkey PRIMARY KEY (provider_connection_id);


--
-- Name: issues issues_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_pkey PRIMARY KEY (id);


--
-- Name: owners owners_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.owners
    ADD CONSTRAINT owners_pkey PRIMARY KEY (id);


--
-- Name: project_repositories project_repositories_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_repositories
    ADD CONSTRAINT project_repositories_pkey PRIMARY KEY (id);


--
-- Name: project_repositories project_repositories_target_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_repositories
    ADD CONSTRAINT project_repositories_target_key UNIQUE (project_id, tracker, repository);


--
-- Name: projects projects_owner_id_slug_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_owner_id_slug_key UNIQUE (owner_id, slug);


--
-- Name: projects projects_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_pkey PRIMARY KEY (id);


--
-- Name: provider_connections provider_connections_app_target_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_connections
    ADD CONSTRAINT provider_connections_app_target_key UNIQUE (app_id, tracker, external_id);


--
-- Name: provider_connections provider_connections_one_owner_per_account; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_connections
    ADD CONSTRAINT provider_connections_one_owner_per_account EXCLUDE USING gist (tracker WITH =, external_id WITH =, owner_id WITH <>);


--
-- Name: provider_connections provider_connections_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_connections
    ADD CONSTRAINT provider_connections_pkey PRIMARY KEY (id);


--
-- Name: report_attachments report_attachments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.report_attachments
    ADD CONSTRAINT report_attachments_pkey PRIMARY KEY (report_id, name);


--
-- Name: report_deliveries report_deliveries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.report_deliveries
    ADD CONSTRAINT report_deliveries_pkey PRIMARY KEY (report_id, project_repository_id);


--
-- Name: reports reports_app_id_report_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reports
    ADD CONSTRAINT reports_app_id_report_id_key UNIQUE (app_id, report_id);


--
-- Name: reports reports_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reports
    ADD CONSTRAINT reports_pkey PRIMARY KEY (id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: app_keys_app_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX app_keys_app_id_idx ON public.app_keys USING btree (app_id);


--
-- Name: app_projects_project_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX app_projects_project_id_idx ON public.app_projects USING btree (project_id);


--
-- Name: apps_owner_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX apps_owner_id_idx ON public.apps USING btree (owner_id);


--
-- Name: issues_project_repository_id_fingerprint_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX issues_project_repository_id_fingerprint_idx ON public.issues USING btree (project_repository_id, fingerprint);


--
-- Name: issues_provider_connection_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX issues_provider_connection_id_idx ON public.issues USING btree (provider_connection_id);


--
-- Name: owners_email_lower_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX owners_email_lower_key ON public.owners USING btree (lower(email));


--
-- Name: project_repositories_one_default_per_project; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX project_repositories_one_default_per_project ON public.project_repositories USING btree (project_id) WHERE is_default;


--
-- Name: project_repositories_project_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX project_repositories_project_id_idx ON public.project_repositories USING btree (project_id);


--
-- Name: provider_connections_app_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_connections_app_id_idx ON public.provider_connections USING btree (app_id);


--
-- Name: provider_connections_owner_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_connections_owner_id_idx ON public.provider_connections USING btree (owner_id);


--
-- Name: reports_app_id_installation_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX reports_app_id_installation_id_idx ON public.reports USING btree (app_id, installation_id);


--
-- Name: reports_not_queued_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX reports_not_queued_idx ON public.reports USING btree (received_at) WHERE (queued_at IS NULL);


--
-- Name: app_keys app_keys_app_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_keys
    ADD CONSTRAINT app_keys_app_id_fkey FOREIGN KEY (app_id) REFERENCES public.apps(id) ON DELETE CASCADE;


--
-- Name: app_projects app_projects_app_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_projects
    ADD CONSTRAINT app_projects_app_id_fkey FOREIGN KEY (app_id) REFERENCES public.apps(id) ON DELETE CASCADE;


--
-- Name: app_projects app_projects_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_projects
    ADD CONSTRAINT app_projects_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: app_tracker_credentials app_tracker_credentials_app_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_tracker_credentials
    ADD CONSTRAINT app_tracker_credentials_app_id_fkey FOREIGN KEY (app_id) REFERENCES public.apps(id) ON DELETE CASCADE;


--
-- Name: apps apps_owner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.apps
    ADD CONSTRAINT apps_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.owners(id) ON DELETE CASCADE;


--
-- Name: connection_secrets connection_secrets_provider_connection_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connection_secrets
    ADD CONSTRAINT connection_secrets_provider_connection_id_fkey FOREIGN KEY (provider_connection_id) REFERENCES public.provider_connections(id) ON DELETE CASCADE;


--
-- Name: issues issues_app_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_app_id_fkey FOREIGN KEY (app_id) REFERENCES public.apps(id) ON DELETE SET NULL;


--
-- Name: issues issues_project_repository_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_project_repository_id_fkey FOREIGN KEY (project_repository_id) REFERENCES public.project_repositories(id) ON DELETE CASCADE;


--
-- Name: issues issues_provider_connection_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_provider_connection_id_fkey FOREIGN KEY (provider_connection_id) REFERENCES public.provider_connections(id) ON DELETE SET NULL;


--
-- Name: project_repositories project_repositories_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_repositories
    ADD CONSTRAINT project_repositories_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: projects projects_owner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.owners(id) ON DELETE CASCADE;


--
-- Name: provider_connections provider_connections_app_owner_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_connections
    ADD CONSTRAINT provider_connections_app_owner_fkey FOREIGN KEY (app_id, owner_id) REFERENCES public.apps(id, owner_id) ON DELETE CASCADE;


--
-- Name: provider_connections provider_connections_owner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_connections
    ADD CONSTRAINT provider_connections_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.owners(id) ON DELETE CASCADE;


--
-- Name: report_attachments report_attachments_report_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.report_attachments
    ADD CONSTRAINT report_attachments_report_id_fkey FOREIGN KEY (report_id) REFERENCES public.reports(id) ON DELETE CASCADE;


--
-- Name: report_deliveries report_deliveries_issue_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.report_deliveries
    ADD CONSTRAINT report_deliveries_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues(id) ON DELETE SET NULL;


--
-- Name: report_deliveries report_deliveries_project_repository_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.report_deliveries
    ADD CONSTRAINT report_deliveries_project_repository_id_fkey FOREIGN KEY (project_repository_id) REFERENCES public.project_repositories(id) ON DELETE CASCADE;


--
-- Name: report_deliveries report_deliveries_report_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.report_deliveries
    ADD CONSTRAINT report_deliveries_report_id_fkey FOREIGN KEY (report_id) REFERENCES public.reports(id) ON DELETE CASCADE;


--
-- Name: reports reports_app_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reports
    ADD CONSTRAINT reports_app_id_fkey FOREIGN KEY (app_id) REFERENCES public.apps(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--



--
-- Dbmate schema migrations
--

INSERT INTO public.schema_migrations (version) VALUES
    ('20261016190043'),
    ('20261016192420'),
    ('20261017120000'),
    ('20261018120000'),
    ('20261021120000'),
    ('20261021130000'),
    ('20261021150000'),
    ('20261022120000'),
    ('20261022210000');
