.PHONY: db-new db-up db-down db-status sqlc-generate proto-generate

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

# Go code for the agent protocol (spec/proto) goes to internal/pb. Needs
# protoc, protoc-gen-go and protoc-gen-go-grpc on PATH.
PB_PKG := github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1;seismov1
PB_MAP := Mseismo/v1/common.proto=$(PB_PKG),Mseismo/v1/agent.proto=$(PB_PKG),Mseismo/v1/coordinator.proto=$(PB_PKG)

proto-generate:
	mkdir -p internal/pb
	protoc -I ../spec/proto \
		--go_out=internal/pb --go_opt='paths=source_relative,$(PB_MAP)' \
		--go-grpc_out=internal/pb --go-grpc_opt='paths=source_relative,$(PB_MAP)' \
		../spec/proto/seismo/v1/*.proto
