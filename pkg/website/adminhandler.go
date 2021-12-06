package website

import (
	"Karaokelist3/pkg/dbaccess"
	"Karaokelist3/pkg/entities"
	"Karaokelist3/pkg/indexer"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/sessions"
)

var store = sessions.NewCookieStore([]byte(os.Getenv("SESSION_KEY")))

func AdminHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	content := renderTemplateContent("admin.html", map[string]interface{}{})
	fullData := map[string]interface{}{
		"Content": template.HTML(content),
	}
	//log.Println(b64.StdEncoding.EncodeToString(securecookie.GenerateRandomKey(32)))
	render(w, r, tmpl, "base.html", fullData)
}

func StartIndenxingHandler(w http.ResponseWriter, r *http.Request, args entities.RunParams) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	//content := renderTemplateContent("admin.html", map[string]interface{}{})
	cdgindexer := indexer.CDGIndexer{}
	error := cdgindexer.ScanFiles(args.KaraokeFolderPath)
	if error != nil {
		log.Fatal(error)
	}
	log.Printf("Number of files: %d", len(cdgindexer.KaraokeFiles))

	sqliteaccess := dbaccess.NewSqliteAccess(args.KaraokeDBPath)
	sqliteaccess.CleanKaraokeFileTable()
	sqliteaccess.InsertKaraokeFiles(cdgindexer.KaraokeFiles)
	fullData := map[string]interface{}{
		"Content": "<p>Indexing files</p>",
	}
	render(w, r, tmpl, "base.html", fullData)
}
