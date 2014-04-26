package main

/*
 * This file is part of theary.
 *
 * theary is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * theary is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with Foobar.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

import (
	"io"
	"strings"
	"compress/gzip"
	"log"
	"net"
	"net/http"
	"net/http/fcgi"
	"html/template"
	"path/filepath"
	"github.com/gorilla/mux"
)

//List of HTML templates
var tmpl *template.Template

//Data passed to a template
type Page struct {
	Title string
}

// Write is a closure for compressing the HTTP output of a web handler function
func makeHandler(fn func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		//w.Header().Set("Access-Control-Allow-Origin", "*")
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			fn(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		gzr := gzipResponseWriter{Writer: gz, ResponseWriter: w}
		fn(gzr, r)
	}
}

type gzipResponseWriter struct {
        io.Writer
        http.ResponseWriter
}

// Write implements a writer for compressing the HTTP output
func (w gzipResponseWriter) Write(b []byte) (int, error) {
        if "" == w.Header().Get("Content-Type") {
			// If no content type, apply sniffing algorithm to un-gzipped body.
			w.Header().Set("Content-Type", http.DetectContentType(b))
        }
        return w.Writer.Write(b)
}

// setup_webui sets up the webui. It can be served by an embedded webserver or thru FastCGI
func setup_webui() {
	tmpl = template.Must(template.ParseFiles(filepath.Join(tmplPath, "home.html")))
	
	r := mux.NewRouter()
	r.HandleFunc("/", makeHandler(homeView))
	r.HandleFunc("/cleo/{query}", makeHandler(searchHandler))
	r.HandleFunc("/recipient/{recipient}", makeHandler(checkRecipientWS))
	r.HandleFunc("/mails/{recipient}", makeHandler(listMailsWS))
	r.HandleFunc("/mails/{recipient}/{id}", makeHandler(getMailWS))
	r.PathPrefix("/").Handler(http.FileServer(http.Dir(staticPath)))

	var err error
	switch strings.ToUpper(gConfig["WEBUI_MODE"]) {
		case "LOCAL":	// Run as a local web server
			err = http.ListenAndServe(gConfig["WEBUI_SERVE"], r)
		case "TCP":		// Serve as FCGI via TCP
			listener, err := net.Listen("tcp", gConfig["WEBUI_SERVE"])
			if err != nil {
				log.Fatal(err)
			}
			defer listener.Close()
		    err = fcgi.Serve(listener, r)
		case "UNIX":		// Run as FCGI via UNIX socket
			listener, err := net.Listen("unix", gConfig["WEBUI_SERVE"])
			if err != nil {
				log.Fatal(err)
			}
			defer listener.Close()
			err = fcgi.Serve(listener, r)
    }
    if err != nil {
        log.Fatal(err)
    }
}

// checkHttpError checks and reports any fatal error. Display an HTTP-500 page
func checkHttpError(err error, w http.ResponseWriter) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatal("%v", err)
	}
}
