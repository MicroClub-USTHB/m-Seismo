package observability

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

type fakeDB struct {
	db.Querier
	apps      map[uuid.UUID]uuid.UUID // appID -> ownerID
	providers map[uuid.UUID]map[string]db.AppObservabilityProvider
}

func newFakeDB() *fakeDB {
	return &fakeDB{
		apps:      make(map[uuid.UUID]uuid.UUID),
		providers: make(map[uuid.UUID]map[string]db.AppObservabilityProvider),
	}
}

func (f *fakeDB) GetApp(_ context.Context, arg db.GetAppParams) (db.App, error) {
	owner, ok := f.apps[arg.ID]
	if !ok || owner != arg.OwnerID {
		return db.App{}, pgx.ErrNoRows
	}
	return db.App{ID: arg.ID, OwnerID: arg.OwnerID}, nil
}

func (f *fakeDB) ListAppObservabilityProviders(_ context.Context, appID uuid.UUID) ([]db.AppObservabilityProvider, error) {
	provMap, ok := f.providers[appID]
	if !ok {
		return nil, nil
	}
	var out []db.AppObservabilityProvider
	for _, p := range provMap {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Provider < out[j].Provider
	})
	return out, nil
}

func (f *fakeDB) GetAppObservabilityProvider(_ context.Context, arg db.GetAppObservabilityProviderParams) (db.AppObservabilityProvider, error) {
	provMap, ok := f.providers[arg.AppID]
	if !ok {
		return db.AppObservabilityProvider{}, pgx.ErrNoRows
	}
	row, ok := provMap[arg.Provider]
	if !ok {
		return db.AppObservabilityProvider{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeDB) SetAppObservabilityProvider(_ context.Context, arg db.SetAppObservabilityProviderParams) (db.AppObservabilityProvider, error) {
	provMap, ok := f.providers[arg.AppID]
	if !ok {
		provMap = make(map[string]db.AppObservabilityProvider)
		f.providers[arg.AppID] = provMap
	}
	row := db.AppObservabilityProvider{
		AppID:     arg.AppID,
		Provider:  arg.Provider,
		Settings:  arg.Settings,
		UpdatedAt: time.Now(),
	}
	provMap[arg.Provider] = row
	return row, nil
}

func (f *fakeDB) DeleteAppObservabilityProvider(_ context.Context, arg db.DeleteAppObservabilityProviderParams) error {
	if provMap, ok := f.providers[arg.AppID]; ok {
		delete(provMap, arg.Provider)
	}
	return nil
}

func (f *fakeDB) DeleteAllAppObservabilityProviders(_ context.Context, appID uuid.UUID) error {
	delete(f.providers, appID)
	return nil
}

func checkKind(t *testing.T, err error, want apperr.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error of kind %v, got nil", want)
	}
	kind, _ := apperr.Classify(err)
	if kind != want {
		t.Fatalf("expected error kind %v, got %v (err: %v)", want, kind, err)
	}
}

func TestService_GetConfigForApp(t *testing.T) {
	ctx := context.Background()
	fdb := newFakeDB()
	svc := NewService(fdb)

	appID := uuid.New()

	// Empty config initially
	cfg, err := svc.GetConfigForApp(ctx, appID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Window != DefaultWindow {
		t.Errorf("expected default window %v, got %v", DefaultWindow, cfg.Window)
	}
	if cfg.Grafana != nil || cfg.Loki != nil || cfg.CloudWatch != nil || cfg.Datadog != nil {
		t.Errorf("expected all providers nil, got %+v", cfg)
	}

	// Add configurations for all 4 providers
	grafanaJSON := `{"base_url":"https://grafana.example.com","datasource_uid":"ds-1"}`
	lokiJSON := `{"base_url":"https://loki.example.com","query_template":"{app=\"foo\"}"}`
	cwJSON := `{"region":"us-east-1","log_group":"/aws/app"}`
	ddJSON := `{"site":"datadoghq.com","query":"service:auth"}`

	_, _ = fdb.SetAppObservabilityProvider(ctx, db.SetAppObservabilityProviderParams{
		AppID: appID, Provider: string(ProviderGrafana), Settings: []byte(grafanaJSON),
	})
	_, _ = fdb.SetAppObservabilityProvider(ctx, db.SetAppObservabilityProviderParams{
		AppID: appID, Provider: string(ProviderLoki), Settings: []byte(lokiJSON),
	})
	_, _ = fdb.SetAppObservabilityProvider(ctx, db.SetAppObservabilityProviderParams{
		AppID: appID, Provider: string(ProviderCloudWatch), Settings: []byte(cwJSON),
	})
	_, _ = fdb.SetAppObservabilityProvider(ctx, db.SetAppObservabilityProviderParams{
		AppID: appID, Provider: string(ProviderDatadog), Settings: []byte(ddJSON),
	})

	cfg, err = svc.GetConfigForApp(ctx, appID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Grafana == nil || cfg.Grafana.BaseURL != "https://grafana.example.com" || cfg.Grafana.DatasourceUID != "ds-1" {
		t.Errorf("unexpected Grafana config: %+v", cfg.Grafana)
	}
	if cfg.Loki == nil || cfg.Loki.BaseURL != "https://loki.example.com" {
		t.Errorf("unexpected Loki config: %+v", cfg.Loki)
	}
	if cfg.CloudWatch == nil || cfg.CloudWatch.LogGroup != "/aws/app" || cfg.CloudWatch.Region != "us-east-1" {
		t.Errorf("unexpected CloudWatch config: %+v", cfg.CloudWatch)
	}
	if cfg.Datadog == nil || cfg.Datadog.Query != "service:auth" {
		t.Errorf("unexpected Datadog config: %+v", cfg.Datadog)
	}

	// Corrupt JSON in settings returns an error
	_, _ = fdb.SetAppObservabilityProvider(ctx, db.SetAppObservabilityProviderParams{
		AppID: appID, Provider: string(ProviderGrafana), Settings: []byte(`{invalid-json`),
	})
	_, err = svc.GetConfigForApp(ctx, appID)
	if err == nil {
		t.Fatal("expected error on corrupt json, got nil")
	}
}

func TestService_List(t *testing.T) {
	ctx := context.Background()
	fdb := newFakeDB()
	svc := NewService(fdb)

	ownerID := uuid.New()
	appID := uuid.New()
	otherOwnerID := uuid.New()

	// App does not exist yet
	_, err := svc.List(ctx, ownerID, appID)
	checkKind(t, err, apperr.KindNotFound)

	// Register app
	fdb.apps[appID] = ownerID

	// Wrong owner
	_, err = svc.List(ctx, otherOwnerID, appID)
	checkKind(t, err, apperr.KindNotFound)

	// Correct owner, empty list
	list, err := svc.List(ctx, ownerID, appID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list == nil || len(list) != 0 {
		t.Errorf("expected empty non-nil slice, got: %v", list)
	}

	// Add a provider
	grafanaJSON := `{"base_url":"https://grafana.example.com"}`
	_, _ = fdb.SetAppObservabilityProvider(ctx, db.SetAppObservabilityProviderParams{
		AppID: appID, Provider: string(ProviderGrafana), Settings: []byte(grafanaJSON),
	})

	list, err = svc.List(ctx, ownerID, appID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 || list[0].Provider != ProviderGrafana {
		t.Fatalf("expected 1 connection with provider grafana, got: %+v", list)
	}
}

func TestService_Get(t *testing.T) {
	ctx := context.Background()
	fdb := newFakeDB()
	svc := NewService(fdb)

	ownerID := uuid.New()
	appID := uuid.New()
	fdb.apps[appID] = ownerID

	// Not configured yet
	_, err := svc.Get(ctx, ownerID, appID, ProviderGrafana)
	checkKind(t, err, apperr.KindNotFound)

	// Configure Grafana
	grafanaJSON := `{"base_url":"https://grafana.example.com"}`
	_, _ = fdb.SetAppObservabilityProvider(ctx, db.SetAppObservabilityProviderParams{
		AppID: appID, Provider: string(ProviderGrafana), Settings: []byte(grafanaJSON),
	})

	conn, err := svc.Get(ctx, ownerID, appID, ProviderGrafana)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn.Provider != ProviderGrafana {
		t.Errorf("expected provider grafana, got %s", conn.Provider)
	}

	// Wrong owner
	_, err = svc.Get(ctx, uuid.New(), appID, ProviderGrafana)
	checkKind(t, err, apperr.KindNotFound)
}

func TestService_Set_Validation(t *testing.T) {
	ctx := context.Background()
	fdb := newFakeDB()
	svc := NewService(fdb)

	ownerID := uuid.New()
	appID := uuid.New()
	fdb.apps[appID] = ownerID

	tests := []struct {
		name     string
		owner    uuid.UUID
		app      uuid.UUID
		provider Provider
		settings string
		wantKind apperr.Kind
	}{
		{
			name:     "unauthorized / unknown app",
			owner:    uuid.New(),
			app:      appID,
			provider: ProviderGrafana,
			settings: `{"base_url":"https://grafana.example.com"}`,
			wantKind: apperr.KindNotFound,
		},
		{
			name:     "unsupported provider",
			owner:    ownerID,
			app:      appID,
			provider: "kibana",
			settings: `{"base_url":"https://kibana.example.com"}`,
			wantKind: apperr.KindInvalid,
		},
		{
			name:     "empty settings",
			owner:    ownerID,
			app:      appID,
			provider: ProviderGrafana,
			settings: ``,
			wantKind: apperr.KindInvalid,
		},
		{
			name:     "malformed json",
			owner:    ownerID,
			app:      appID,
			provider: ProviderGrafana,
			settings: `{"base_url":`,
			wantKind: apperr.KindInvalid,
		},
		{
			name:     "json not an object",
			owner:    ownerID,
			app:      appID,
			provider: ProviderGrafana,
			settings: `[1, 2, 3]`,
			wantKind: apperr.KindInvalid,
		},
		{
			name:     "grafana missing base_url",
			owner:    ownerID,
			app:      appID,
			provider: ProviderGrafana,
			settings: `{"base_url":"  "}`,
			wantKind: apperr.KindInvalid,
		},
		{
			name:     "grafana valid",
			owner:    ownerID,
			app:      appID,
			provider: ProviderGrafana,
			settings: `{"base_url":"https://grafana.example.com"}`,
			wantKind: -1, // success
		},
		{
			name:     "loki missing base_url",
			owner:    ownerID,
			app:      appID,
			provider: ProviderLoki,
			settings: `{"query_template":"{app=\"foo\"}"}`,
			wantKind: apperr.KindInvalid,
		},
		{
			name:     "loki valid",
			owner:    ownerID,
			app:      appID,
			provider: ProviderLoki,
			settings: `{"base_url":"https://loki.example.com"}`,
			wantKind: -1, // success
		},
		{
			name:     "cloudwatch missing log_group",
			owner:    ownerID,
			app:      appID,
			provider: ProviderCloudWatch,
			settings: `{"region":"us-east-1"}`,
			wantKind: apperr.KindInvalid,
		},
		{
			name:     "cloudwatch valid",
			owner:    ownerID,
			app:      appID,
			provider: ProviderCloudWatch,
			settings: `{"region":"us-east-1","log_group":"/aws/app"}`,
			wantKind: -1, // success
		},
		{
			name:     "datadog valid",
			owner:    ownerID,
			app:      appID,
			provider: ProviderDatadog,
			settings: `{"site":"datadoghq.eu","query":"service:core"}`,
			wantKind: -1, // success
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.Set(ctx, tt.owner, tt.app, tt.provider, json.RawMessage(tt.settings))
			if tt.wantKind == -1 {
				if err != nil {
					t.Fatalf("expected success, got error: %v", err)
				}
			} else {
				checkKind(t, err, tt.wantKind)
			}
		})
	}
}

func TestService_Delete_and_DeleteAll(t *testing.T) {
	ctx := context.Background()
	fdb := newFakeDB()
	svc := NewService(fdb)

	ownerID := uuid.New()
	appID := uuid.New()
	fdb.apps[appID] = ownerID

	// Seed providers
	_ = svc.Set(ctx, ownerID, appID, ProviderGrafana, json.RawMessage(`{"base_url":"https://grafana.example.com"}`))
	_ = svc.Set(ctx, ownerID, appID, ProviderLoki, json.RawMessage(`{"base_url":"https://loki.example.com"}`))

	// Delete Grafana
	if err := svc.Delete(ctx, ownerID, appID, ProviderGrafana); err != nil {
		t.Fatalf("delete grafana failed: %v", err)
	}

	list, err := svc.List(ctx, ownerID, appID)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 1 || list[0].Provider != ProviderLoki {
		t.Fatalf("expected only loki remaining, got: %+v", list)
	}

	// Delete from wrong owner
	if err := svc.Delete(ctx, uuid.New(), appID, ProviderLoki); err == nil {
		t.Fatal("expected error deleting with wrong owner, got nil")
	}

	// DeleteAll
	if err := svc.DeleteAll(ctx, ownerID, appID); err != nil {
		t.Fatalf("delete all failed: %v", err)
	}

	list, err = svc.List(ctx, ownerID, appID)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list after DeleteAll, got %d items", len(list))
	}
}
