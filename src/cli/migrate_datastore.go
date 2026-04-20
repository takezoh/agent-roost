package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"cloud.google.com/go/datastore"
	admin "cloud.google.com/go/datastore/admin/apiv1"
	adminpb "cloud.google.com/go/datastore/admin/apiv1/adminpb"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func init() {
	Register("migrate-datastore", "Migrate data using Datastore Admin API", RunMigrateDatastore)
}

// GameUserEntity represents a GameUser entity in Datastore.
type GameUserEntity struct {
	UserID    string    `datastore:"user_id"`
	CreatedAt time.Time `datastore:"created_at"`
}

// PlayerMetadataEntity represents a PlayerMetadata entity in Datastore.
type PlayerMetadataEntity struct {
	UserID    string    `datastore:"user_id"`
	CreatedAt time.Time `datastore:"created_at"`
	// Add other fields for PlayerMetadata if necessary
}

// RunMigrateDatastore implements `roost migrate-datastore <gcs-export-path> <project-id> <location>`.
func RunMigrateDatastore(args []string) error { //nolint:funlen
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: roost migrate-datastore <gcs-export-path> <project-id> <location>")
		return errors.New("migrate-datastore: incorrect number of arguments")
	}

	gcsExportPath := args[0]
	projectID := args[1]
	location := args[2]

	ctx := context.Background()

	// Determine if Datastore Emulator is running
	datastoreEmulatorHost := os.Getenv("DATASTORE_EMULATOR_HOST")
	var opts []option.ClientOption
	if datastoreEmulatorHost != "" {
		slog.Info("Using Datastore Emulator", "host", datastoreEmulatorHost)
		opts = append(opts, option.WithEndpoint(datastoreEmulatorHost), option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	}

	// 1. Datastore Admin Import
	slog.Info("Starting Datastore Admin Import", "path", gcsExportPath, "project", projectID, "location", location)
	adminClient, err := admin.NewDatastoreAdminClient(ctx, opts...)
	if err != nil {
		return fmt.Errorf("migrate-datastore: failed to create datastore admin client: %w", err)
	}
	defer adminClient.Close()

	op, err := adminClient.ImportEntities(ctx, &adminpb.ImportEntitiesRequest{
		ProjectId:    projectID,
		InputUrl:     gcsExportPath,
		EntityFilter: &adminpb.EntityFilter{Kinds: []string{"GameUser"}},
	})
	if err != nil {
		return fmt.Errorf("migrate-datastore: failed to start import operation: %w", err)
	}

	// Wait for the import operation to complete
	slog.Info("Waiting for import operation to complete...")
	err = op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("migrate-datastore: import operation failed: %w", err)
	}
	slog.Info("Datastore Admin Import completed successfully.")

	// 2. Query all GameUser entities
	slog.Info("Querying GameUser entities...")
	dsClient, err := datastore.NewClient(ctx, projectID, opts...)
	if err != nil {
		return fmt.Errorf("migrate-datastore: failed to create datastore client: %w", err)
	}
	defer dsClient.Close()

	var gameUsers []*GameUserEntity
	query := datastore.NewQuery("GameUser")
	_, err = dsClient.GetAll(ctx, query, &gameUsers)
	if err != nil {
		return fmt.Errorf("migrate-datastore: failed to query GameUser entities: %w", err)
	}
	slog.Info("GameUser entities queried", "count", len(gameUsers))

	// 3. Create PlayerMetadata entities
	slog.Info("Creating PlayerMetadata entities...")

	// Batch put operations
	const batchSize = 500 // Max batch size for Datastore is 500
	var playerMetadataKeys []*datastore.Key
	var playerMetadataEntities []*PlayerMetadataEntity

	for i, gu := range gameUsers {
		key := datastore.NameKey("PlayerMetadata", gu.UserID, nil) // Use UserID as name for uniqueness
		playerMetadataKeys = append(playerMetadataKeys, key)
		playerMetadataEntities = append(playerMetadataEntities, &PlayerMetadataEntity{
			UserID:    gu.UserID,
			CreatedAt: gu.CreatedAt,
		})

		if (i+1)%batchSize == 0 || i == len(gameUsers)-1 {
			// Check for existence and skip if already exists (optional, depending on requirements)
			// For simplicity, we'll just attempt to put. If a key already exists, it will be overwritten.
			// If you need to skip existing entities, you'd need to fetch them first.

			if _, err := dsClient.PutMulti(ctx, playerMetadataKeys, playerMetadataEntities); err != nil {
				return fmt.Errorf("migrate-datastore: failed to put PlayerMetadata batch: %w", err)
			}
			slog.Info("PlayerMetadata batch created", "count", len(playerMetadataEntities))
			playerMetadataKeys = nil
			playerMetadataEntities = nil
		}
	}
	slog.Info("PlayerMetadata entities created successfully.")

	// 4. Delete GameUser entities
	slog.Info("Deleting GameUser entities...")
	var gameUserKeys []*datastore.Key
	for i, gu := range gameUsers {
		key := datastore.NameKey("GameUser", gu.UserID, nil)
		gameUserKeys = append(gameUserKeys, key)

		if (i+1)%batchSize == 0 || i == len(gameUsers)-1 {
			if err := dsClient.DeleteMulti(ctx, gameUserKeys); err != nil {
				return fmt.Errorf("migrate-datastore: failed to delete GameUser batch: %w", err)
			}
			slog.Info("GameUser batch deleted", "count", len(gameUserKeys))
			gameUserKeys = nil
		}
	}
	slog.Info("GameUser entities deleted successfully.")

	return nil
}
