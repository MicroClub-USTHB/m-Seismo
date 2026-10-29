package crashprovider

import (
	"context"
	"fmt"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

// RotateCredentials rewrites saved tokens with the active encryption key.
// Previous keys remain usable during a rolling deployment. Compare-and-swap
// updates prevent a concurrent owner token replacement from being overwritten.
// A partial run is safe to repeat; remove previous keys only after success.
func (s *Service) RotateCredentials(ctx context.Context) (int, error) {
	if s.vault.AEAD == nil {
		return 0, fmt.Errorf("credential rotation requires an active encryption key")
	}
	rows, err := s.q.ListAppCrashProviderCredentials(ctx)
	if err != nil {
		return 0, fmt.Errorf("list crash credentials: %w", err)
	}
	count := 0
	for _, row := range rows {
		token, err := s.vault.open(row.AppID, row.Credentials)
		if err != nil {
			return count, err
		}
		encrypted, err := s.vault.seal(row.AppID, token)
		if err != nil {
			return count, err
		}
		changed, err := s.q.RotateAppCrashProviderCredentials(ctx, db.RotateAppCrashProviderCredentialsParams{
			AppID: row.AppID, NewCredentials: encrypted, OldCredentials: row.Credentials,
		})
		if err != nil {
			return count, fmt.Errorf("rotate crash credentials: %w", err)
		}
		if changed != 1 {
			return count, fmt.Errorf("a crash connection changed concurrently; retry credential rotation")
		}
		count++
	}
	return count, nil
}
