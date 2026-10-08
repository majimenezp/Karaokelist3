package catalog

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkCatalogView50k(b *testing.B) {
	repo, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	defer repo.Close()
	tx, err := repo.db.BeginTx(context.Background(), nil)
	if err != nil {
		b.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO tracks(cdg_path,mp3_path,title,artist,genre,search_key) VALUES(?,?,?,?,?,?)`)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 50000; i++ {
		artist := fmt.Sprintf("Artist %03d", i%500)
		genre := fmt.Sprintf("Genre %02d", i%12)
		title := fmt.Sprintf("Song %05d", i)
		if _, err := stmt.Exec(fmt.Sprintf("/%d.cdg", i), fmt.Sprintf("/%d.mp3", i), title, artist, genre, Normalize(title+" "+artist+" "+genre)); err != nil {
			b.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		b.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := repo.Search(ctx, "song 01234", "", "", 100, 0); err != nil {
			b.Fatal(err)
		}
		if _, err := repo.Facets(ctx, "artist"); err != nil {
			b.Fatal(err)
		}
		if _, err := repo.Facets(ctx, "genre"); err != nil {
			b.Fatal(err)
		}
		if _, err := repo.Count(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
