package website

import (
	"Karaokelist3/pkg/dbaccess"
	"Karaokelist3/pkg/entities"
	"html/template"
	"net/http"
)

func MainHandler(w http.ResponseWriter, r *http.Request) {
	//push(w, "/static/style.css")
	//push(w, "/static/navigation_bar.css")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	content := renderTemplateContent("main.html", map[string]interface{}{})
	fullData := map[string]interface{}{
		"Content": template.HTML(content),
	}
	render(w, r, tmpl, "base.html", fullData)
}

func SearchPostHandler(w http.ResponseWriter, r *http.Request, args entities.RunParams) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	ciname := r.PostFormValue("ci_name")
	cisearch := r.PostFormValue("ci_search")
	sqliteaccess := dbaccess.NewSqliteAccess(args.KaraokeDBPath)
	results := sqliteaccess.SearchKaraokeFiles("la chica de humo")
	content := renderTemplateContent("results.html", map[string]interface{}{
		"cisearch":   cisearch,
		"ciname":     ciname,
		"results":    results,
		"resultsLen": len(results),
	})
	fullData := map[string]interface{}{
		"Content": template.HTML(content),
	}
	render(w, r, tmpl, "base.html", fullData)
}
