package server

import (
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin/render"
)

// HTMLRenderer implements gin.HTMLRender
type HTMLRenderer struct {
	templates map[string]*template.Template
}

func (r *HTMLRenderer) Instance(name string, data interface{}) render.Render {
	tmpl, ok := r.templates[name]
	return &HTMLRender{
		template: tmpl,
		name:     name,
		data:     data,
		exists:   ok,
	}
}

// HTMLRender implements render.Render
type HTMLRender struct {
	template *template.Template
	name     string
	data     interface{}
	exists   bool
}

func (r *HTMLRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	if !r.exists || r.template == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte("Template not found: " + r.name))
		return err
	}
	return r.template.ExecuteTemplate(w, r.name, r.data)
}

func (r *HTMLRender) WriteContentType(w http.ResponseWriter) {
	header := w.Header()
	if val := header["Content-Type"]; len(val) == 0 {
		header["Content-Type"] = []string{"text/html; charset=utf-8"}
	}
}
