package cli

import (
	"context"
	"fmt" // fmt をインポート
	"log/slog"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/datastore"
	"github.com/stretchr/testify/assert"
	"google.golang.org/api/iterator" // iterator をインポート
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestRunMigrateDatastoreEmulator tests the migration logic using Datastore Emulator.
// Note: Datastore Admin Import (ImportEntities) cannot be fully tested with the emulator,
// as the emulator does not emulate GCS bucket access for imports.
// This test focuses on the query, create, and delete operations after an "import".
func TestRunMigrateDatastoreEmulator(t *testing.T) {
	// Ensure DATASTORE_EMULATOR_HOST is set for the test
	emulatorHost := os.Getenv("DATASTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("DATASTORE_EMULATOR_HOST not set. Skipping emulator test.")
	}

	projectID := "test-project" // Emulator often uses a dummy project ID
	location := "test-location"

	// Setup Datastore Client for emulator
	ctx := context.Background()
	var opts []option.ClientOption
	opts = append(opts, option.WithEndpoint(emulatorHost), option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))

	dsClient, err := datastore.NewClient(ctx, projectID, opts...)
	assert.NoError(t, err)
	defer dsClient.Close()

	// Clear existing data in the emulator for a clean test run
	err = clearEmulatorData(ctx, dsClient, projectID)
	assert.NoError(t, err, "Failed to clear emulator data")

	// --- Simulate "Import" by directly putting GameUser entities ---
	slog.Info("Simulating GameUser import into emulator...")
	mockGameUsers := []struct {
		UserID    string
		CreatedAt time.Time
	}{
		{UserID: "user1", CreatedAt: time.Now().Add(-24 * time.Hour)},
		{UserID: "user2", CreatedAt: time.Now().Add(-48 * time.Hour)},
		{UserID: "user3", CreatedAt: time.Now().Add(-72 * time.Hour)},
	}

	var gameUserKeys []*datastore.Key
	var gameUserEntities []GameUserEntity // GameUserEntity を使用
	for _, u := range mockGameUsers {
		key := datastore.NameKey("GameUser", u.UserID, nil)
		gameUserKeys = append(gameUserKeys, key)
		gameUserEntities = append(gameUserEntities, GameUserEntity{UserID: u.UserID, CreatedAt: u.CreatedAt})
	}

	_, err = dsClient.PutMulti(ctx, gameUserKeys, gameUserEntities)
	assert.NoError(t, err, "Failed to put mock GameUser entities")
	slog.Info("Mock GameUser entities put successfully.")

	// --- Run the migration command ---
	// The GCS path here is a dummy, as ImportEntities is not executed against emulator.
	dummyGCSPath := "gs://dummy-bucket/dummy-export"
	err = RunMigrateDatastore([]string{dummyGCSPath, projectID, location})
	assert.NoError(t, err, "RunMigrateDatastore failed")

	// --- Verify results ---

	// Check if PlayerMetadata entities were created
	slog.Info("Verifying PlayerMetadata entities...")
	var playerMetadata []*PlayerMetadataEntity // PlayerMetadataEntity を使用
	pmQuery := datastore.NewQuery("PlayerMetadata")
	_, err = dsClient.GetAll(ctx, pmQuery, &playerMetadata)
	assert.NoError(t, err)
	assert.Len(t, playerMetadata, len(mockGameUsers), "Expected PlayerMetadata entities to be created")

	// Basic check for content
	foundUserIDs := make(map[string]bool)
	for _, pm := range playerMetadata {
		foundUserIDs[pm.UserID] = true
	}
	for _, mu := range mockGameUsers {
		assert.True(t, foundUserIDs[mu.UserID], "Expected PlayerMetadata for user %s not found", mu.UserID)
	}
	slog.Info("PlayerMetadata entities verified.")

	// Check if GameUser entities were deleted
	slog.Info("Verifying GameUser entities deletion...")
	var remainingGameUsers []*GameUserEntity // GameUserEntity を使用
	guQuery := datastore.NewQuery("GameUser")
	_, err = dsClient.GetAll(ctx, guQuery, &remainingGameUsers)
	assert.NoError(t, err)
	assert.Len(t, remainingGameUsers, 0, "Expected all GameUser entities to be deleted")
	slog.Info("GameUser entities deletion verified.")
}

// clearEmulatorData clears all entities for a given project in the Datastore Emulator.
func clearEmulatorData(ctx context.Context, client *datastore.Client, projectID string) error {
	slog.Info("Clearing emulator data for project", "projectID", projectID)
	kinds, err := getAllKinds(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to get all kinds: %w", err)
	}

	for _, kind := range kinds {
		q := datastore.NewQuery(kind).KeysOnly()
		keys, err := client.GetAll(ctx, q, nil)
		if err != nil {
			return fmt.Errorf("failed to get keys for kind %s: %w", kind, err)
		}
		if len(keys) > 0 {
			err = client.DeleteMulti(ctx, keys)
			if err != nil {
				return fmt.Errorf("failed to delete entities for kind %s: %w", kind, err)
			}
			slog.Info("Deleted entities for kind", "kind", kind, "count", len(keys))
		}
	}
	slog.Info("Emulator data cleared.")
	return nil
}

// getAllKinds retrieves all unique kinds in the Datastore Emulator for a given project.
func getAllKinds(ctx context.Context, client *datastore.Client) ([]string, error) {
	q := datastore.NewQuery("__kind__").KeysOnly()
	iter := client.Run(ctx, q)
	var kinds []string
	for {
		var key *datastore.Key
		_, err := iter.Next(&key)
		if err == iterator.Done { // iterator.Done を使用
			break
		}
		if err != nil {
			return nil, err
		}
		kinds = append(kinds, key.Name) // key.Name を使用
	}
	return kinds, nil
}
