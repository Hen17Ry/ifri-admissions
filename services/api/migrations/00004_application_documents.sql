-- +goose Up
CREATE TABLE application_documents (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 application_id uuid NOT NULL REFERENCES applications(id),
 document_type text NOT NULL CHECK (document_type IN ('REQUEST_LETTER','TRANSCRIPT','IDENTITY_DOCUMENT','PARENTAL_COMMITMENT')),
 current_version_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(application_id,document_type)
);
CREATE TABLE document_versions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 application_document_id uuid NOT NULL REFERENCES application_documents(id),
 version_number integer NOT NULL CHECK (version_number>0),
 storage_key text NOT NULL UNIQUE CHECK (storage_key ~ '^applications/[a-f0-9-]{36}/documents/(REQUEST_LETTER|TRANSCRIPT|IDENTITY_DOCUMENT|PARENTAL_COMMITMENT)/[a-f0-9-]{36}\.pdf$'),
 original_filename text NOT NULL CHECK (length(original_filename) BETWEEN 1 AND 200 AND strpos(original_filename,chr(10))=0 AND strpos(original_filename,chr(13))=0),
 content_type text NOT NULL CHECK (content_type='application/pdf'),
 size_bytes bigint NOT NULL CHECK (size_bytes>0),
 sha256 text NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT now(),
 replaced_at timestamptz,
 UNIQUE(application_document_id,version_number),
 UNIQUE(id,application_document_id),
 CHECK (replaced_at IS NULL OR replaced_at>=created_at)
);
CREATE UNIQUE INDEX document_single_current ON document_versions(application_document_id) WHERE replaced_at IS NULL;
ALTER TABLE application_documents ADD CONSTRAINT document_current_owner FOREIGN KEY(current_version_id,id) REFERENCES document_versions(id,application_document_id) DEFERRABLE INITIALLY DEFERRED;
-- Enforce agreement of pointer and live version at commit, including direct SQL writes.
-- +goose StatementBegin
CREATE FUNCTION check_document_current() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE document_id uuid;
BEGIN
 IF TG_TABLE_NAME='application_documents' THEN document_id:=NEW.id;
 ELSE
  IF TG_OP='DELETE' THEN document_id:=OLD.application_document_id; ELSE document_id:=NEW.application_document_id; END IF;
 END IF;
 IF EXISTS (SELECT 1 FROM application_documents d LEFT JOIN document_versions v ON v.id=d.current_version_id AND v.application_document_id=d.id WHERE d.id=document_id AND (v.id IS NULL OR v.replaced_at IS NOT NULL)) THEN
  RAISE EXCEPTION 'Current document version is missing or superseded' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER document_pointer_check AFTER INSERT OR UPDATE ON application_documents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_document_current();
CREATE CONSTRAINT TRIGGER document_version_check AFTER INSERT OR UPDATE OR DELETE ON document_versions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_document_current();
-- +goose StatementEnd
-- +goose Down
ALTER TABLE application_documents DROP CONSTRAINT document_current_owner;
DROP TABLE document_versions;
DROP TABLE application_documents;
DROP FUNCTION check_document_current();
