package store

import (
	"context"
	"database/sql"
	"embed"

	"github.com/XSAM/otelsql"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
	_ "github.com/jackc/pgx/v5/stdlib"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

//go:embed sql/migrations/*.sql
var migrations embed.FS

// RuleTranslator translates a legacy (flat-matcher) routing rule into the
// equivalent conditions-native rules. It is the store's inverted dependency for
// translate-on-read: the eventrouter.SchemaRegistry satisfies it, but the store
// depends only on this interface so it need not import eventrouter (which would
// form an import cycle, since eventrouter imports store).
type RuleTranslator interface {
	TranslateRule(model.LegacyRoutingRule) []model.RoutingRule
}

// Store provides access to all database operations.
type Store struct {
	db *sql.DB

	// Rules translates pre-conditions (legacy) stored routing rules into
	// conditions-native rules on read. It has no default: decodeRoutingRules
	// errors if it encounters a legacy row while Rules is nil. Conditions-native
	// and bare rows decode without a translator. The server startup wiring sets
	// this to eventrouter.DefaultSchemaRegistry.
	Rules RuleTranslator
}

// New creates a new Store with the given database connection.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) q(tx *sql.Tx) *sqlc.Queries {
	if tx != nil {
		return sqlc.New(tx)
	}
	return sqlc.New(s.db)
}

// WithTx runs f within a transaction.
func (s *Store) WithTx(ctx context.Context, tx *sql.Tx, f func(tx *sql.Tx) error) error {
	if tx != nil {
		return f(tx)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return f(tx)
}

func Open(dsn string, migrate bool) (*sql.DB, error) {
	if migrate {
		if err := Migrate(dsn); err != nil {
			return nil, err
		}
	}
	db, err := otelsql.Open("pgx", dsn,
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			DisableErrSkip: true,
		}),
	)
	if err != nil {
		return nil, err
	}
	return db, nil
}
