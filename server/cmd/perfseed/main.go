// Command perfseed fills a tenant with a synthetic corpus for performance tests: documents, passages with their
// word index, and vectors made by the same fake embedding as the fake model servers, so a question about a topic
// finds that topic's passages by words and by meaning. It writes straight to Postgres (no document parsing), as
// the database owner, and the corpus is the same every time. Never run it against real data.
//
//	perfseed -db postgres://atlas_owner:...@127.0.0.1:54329/atlas -tenant UUID -reader UUID -profile pilot -questions q.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	questionSeed = 3
	fileMode     = 0o600
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "perfseed:", err)
		os.Exit(1)
	}
}

type options struct {
	db, tenant, reader, profile, questions string
}

func parse() options {
	var o options
	flag.StringVar(&o.db, "db", "", "owner connection URL of the database")
	flag.StringVar(&o.tenant, "tenant", "", "tenant id (create the organization first with /setup)")
	flag.StringVar(&o.reader, "reader", "", "id of the person who may read the restricted sources")
	flag.StringVar(&o.profile, "profile", "small", "small, pilot, skew or multi-acl")
	flag.StringVar(&o.questions, "questions", "perf-questions.json", "where to write the questions the load test asks")
	flag.Parse()
	return o
}

func run() error {
	o := parse()
	prof, err := profileNamed(o.profile)
	if err != nil {
		return err
	}
	dest, err := o.destination()
	if err != nil {
		return err
	}
	if err := seed(o.db, newLoaderFor(dest, prof)); err != nil {
		return err
	}
	return writeQuestions(o.questions)
}

// seed loads the corpus and reports its size.
func seed(db string, load func(*pgxpool.Pool) *loader) error {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, db)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := load(pool).run(ctx); err != nil {
		return err
	}
	return report(ctx, pool)
}

func newLoaderFor(dest target, prof profile) func(*pgxpool.Pool) *loader {
	return func(pool *pgxpool.Pool) *loader { return newLoader(pool, dest, prof) }
}

func (o options) destination() (target, error) {
	tenant, errTenant := uuid.Parse(o.tenant)
	reader, errReader := uuid.Parse(o.reader)
	if o.db == "" || errTenant != nil || errReader != nil {
		return target{}, fmt.Errorf("-db, -tenant and -reader are required (tenant: %v, reader: %v)", errTenant, errReader)
	}
	return target{tenant: tenant, reader: reader}, nil
}

// report prints how big the corpus is on disk, the numbers a sizing guide needs.
func report(ctx context.Context, pool *pgxpool.Pool) error {
	for _, table := range []string{"documents.documents", "documents.chunks", "documents.chunk_embeddings"} {
		var rows, total, indexes int64
		err := pool.QueryRow(ctx, `SELECT reltuples::bigint, pg_total_relation_size($1::regclass), pg_indexes_size($1::regclass) FROM pg_class WHERE oid = $1::regclass`, table).Scan(&rows, &total, &indexes)
		if err != nil {
			return err
		}
		fmt.Printf("%-28s %9d rows %8d MB total %8d MB indexes\n", table, rows, total>>20, indexes>>20)
	}
	return nil
}

// question is one thing to ask and the topic whose documents answer it.
type question struct {
	Text  string `json:"text"`
	Topic string `json:"topic"`
}

func writeQuestions(path string) error {
	vocab := vocabulary{rng: rand.New(rand.NewPCG(questionSeed, questionSeed+1))}
	var out []question
	for topic := range topics {
		out = append(out, question{Text: vocab.question(topic), Topic: fmt.Sprintf("tema%03d", topic)})
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, fileMode)
}
