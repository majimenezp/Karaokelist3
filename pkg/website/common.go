package website

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var tmpl *template.Template

func push(w http.ResponseWriter, resource string) {
	pusher, ok := w.(http.Pusher)
	if ok {
		if err := pusher.Push(resource, nil); err == nil {
			return
		}
	}
}

func render(w http.ResponseWriter, r *http.Request, tpl *template.Template, name string, data interface{}) {
	buf := new(bytes.Buffer)
	if err := tpl.ExecuteTemplate(buf, name, data); err != nil {
		fmt.Printf("\nRender Error: %v\n", err)
		return
	}
	w.Write(buf.Bytes())
}

func findAndParseTemplates(rootDir string, funcMap template.FuncMap) (*template.Template, error) {
	cleanRoot := filepath.Clean(rootDir)
	pfx := len(cleanRoot) + 1
	root := template.New("")

	err := filepath.Walk(cleanRoot, func(path string, info os.FileInfo, e1 error) error {
		if !info.IsDir() && strings.HasSuffix(path, ".html") {
			if e1 != nil {
				return e1
			}

			b, e2 := ioutil.ReadFile(path)
			if e2 != nil {
				return e2
			}

			name := path[pfx:]
			t := root.New(name).Funcs(funcMap)
			_, e2 = t.Parse(string(b))
			if e2 != nil {
				return e2
			}
		}

		return nil
	})

	return root, err
}

func WebsiteInit() {
	tmpl = template.New("base")
	//currentPath, err := os.Getwd()
	//if err != nil {
	//	log.Fatal(err)
	//}
	//path.Join(currentPath, "web/templates/*.html")
	_, err := tmpl.ParseGlob("web/templates/*.html")
	if err != nil {
		log.Fatal("Error loading templates:" + err.Error())
	}
}

func renderTemplateContent(name string, data map[string]interface{}) string {
	var tpl bytes.Buffer
	if err := tmpl.ExecuteTemplate(&tpl, name, data); err != nil {
		log.Fatal("Error loading templates:" + err.Error())
	}
	return tpl.String()
}
