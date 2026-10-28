.PHONY: db-new db-up db-down db-status sqlc-generate

DBMATE := dbmate --migrations-dir ./db/migrations --schema-file ./db/schema.sql

db-new:
	$(DBMATE) new $(name)

db-up:
	$(DBMATE) up
	@# pg_dump 18+ wraps dumps in \restrict/\unrestrict guards that sqlc's parser rejects.
	@sed -i '' '/^\\restrict /d; /^\\unrestrict /d' ./db/schema.sql

db-down:
	$(DBMATE) down
	@sed -i '' '/^\\restrict /d; /^\\unrestrict /d' ./db/schema.sql

db-status:
	$(DBMATE) status

sqlc-generate:
	sqlc generate
