package store

import (
	"net/url"

	"github.com/amacneil/dbmate/v2/pkg/dbmate"
	_ "github.com/amacneil/dbmate/v2/pkg/driver/postgres"
)

func migrate(databaseURL string) error {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return err
	}
	db := dbmate.New(u)
	db.FS = migrations
	db.MigrationsDir = []string{"sql/migrations"}
	db.AutoDumpSchema = false
	return db.Migrate()
}
