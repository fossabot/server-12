package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/mattn/go-sqlite3"
	"github.com/meteorae/meteorae-server/database/models"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

type entrypoint struct {
	lib  string
	proc string
}

var libNames = []entrypoint{
	{"./libgo-sqlite3-spellfix", "sqlite3_spellfix_init"},
}

var errLibraryNotFound = errors.New("libgo-sqlite3-spellfix not found")

func GetDatabase(zerologger zerolog.Logger) error {
	newLogger := logger.New(
		&zerologger,
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Info,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)

	sql.Register("sqlite3-spellfix", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			for _, v := range libNames {
				err := conn.LoadExtension(v.lib, v.proc)
				if err == nil {
					return nil
				}
				log.Err(err)
			}
			err := errLibraryNotFound

			return err
		},
	})

	var err error // Linters complain if we initilize this on the next line
	DB, err = gorm.Open(&sqlite.Dialector{DriverName: "sqlite3-spellfix", DSN: "meteorae.db"}, &gorm.Config{
		Logger: newLogger,
	})

	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	return nil
}

func Migrate() error {
	migrations := gormigrate.New(DB, gormigrate.DefaultOptions, []*gormigrate.Migration{})

	migrations.InitSchema(func(transaction *gorm.DB) error {
		err := transaction.AutoMigrate(
			&models.Account{},
			&models.ExternalIdentifier{},
			&models.Library{},
			&models.LibraryLocation{},
			&models.MediaPart{},
			&models.ItemMetadata{},
			&models.MediaStream{},
		)
		if err != nil {
			return fmt.Errorf("failed to run database initialization migrations: %w", err)
		}

		// Create the virtual tables
		result := transaction.Exec(
			/* sql */ `CREATE VIRTUAL TABLE fts4_item_metadata USING fts4(
				content=item_metadata,
				title,
				sort_title,
				original_title,
				tokenize=icu
			);`)
		if result.Error != nil {
			return fmt.Errorf("failed to create virtual table: %w", result.Error)
		}

		result = transaction.Exec(
			/* sql */ `CREATE VIRTUAL TABLE fts4_item_metadata_terms USING fts4aux(
				fts4_item_metadata
			);`)
		if result.Error != nil {
			return fmt.Errorf("failed to create virtual table: %w", result.Error)
		}

		result = transaction.Exec( /* sql */ `CREATE VIRTUAL TABLE spellfix_metadata_titles USING spellfix1;`)
		if result.Error != nil {
			return fmt.Errorf("failed to create virtual table: %w", result.Error)
		}

		// fts4 triggers
		result = transaction.Exec(
			/* sql */ `CREATE TRIGGER fts4_item_metadata_after_insert
			AFTER
			INSERT ON item_metadata BEGIN
			INSERT INTO fts4_item_metadata(rowid, title, sort_title, original_title)
			VALUES (
					new.id,
					new.title,
					new.sort_title,
					new.original_title
				);
			INSERT INTO spellfix_metadata_titles(word)
			SELECT term
			FROM fts4_item_metadata_terms
			WHERE col='*'
				AND term not in (
					SELECT word
					from spellfix_metadata_titles
				);
			END;`)
		if result.Error != nil {
			return fmt.Errorf("failed to create trigger: %w", result.Error)
		}

		result = transaction.Exec(
			/* sql */ `CREATE TRIGGER fts4_item_metadata_before_update BEFORE
			UPDATE ON item_metadata BEGIN
			DELETE FROM fts4_item_metadata
			WHERE docid=old.rowid;
			END;`)
		if result.Error != nil {
			return fmt.Errorf("failed to create trigger: %w", result.Error)
		}

		result = transaction.Exec(
			/* sql */ `CREATE TRIGGER fts4_item_metadata_before_delete BEFORE DELETE ON item_metadata BEGIN
			DELETE FROM fts4_item_metadata
			WHERE docid=old.rowid;
			END;`)
		if result.Error != nil {
			return fmt.Errorf("failed to create trigger: %w", result.Error)
		}

		result = transaction.Exec(
			/* sql */ `CREATE TRIGGER fts4_item_metadata_after_update
			AFTER
			UPDATE ON item_metadata BEGIN
			INSERT INTO fts4_item_metadata(rowid, title, sort_title, original_title)
			VALUES (
					new.id,
					new.title,
					new.sort_title,
					new.original_title
				);
			END;`)
		if result.Error != nil {
			return fmt.Errorf("failed to create trigger: %w", result.Error)
		}

		return nil
	})

	if err := migrations.Migrate(); errors.Is(err, nil) {
		return fmt.Errorf("could not migrate: %w", err)
	}

	return fmt.Errorf("failed to run automatic migrations: %w", DB.AutoMigrate())
}
