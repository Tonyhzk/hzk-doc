package server

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func (s *Server) requestFilePath(r *http.Request) (string, error) {
	if id := r.URL.Query().Get("id"); id != "" {
		current := s.current()
		if current != nil {
			if meta, ok := current.byID[id]; ok {
				return meta.Path, nil
			}
		}
		return "", os.ErrNotExist
	}
	name, err := cleanRequestPath(r.URL.Query().Get("path"))
	if err != nil {
		return "", err
	}
	if doc, err := s.catalog.FindPath(name); err == nil {
		return doc.Path, nil
	}
	return name, nil
}

func (s *Server) documentLinks(source string, full bool) string {
	var nodes []*html.Node
	if full {
		doc, err := html.Parse(strings.NewReader(source))
		if err != nil {
			return source
		}
		nodes = []*html.Node{doc}
	} else {
		context := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
		var err error
		nodes, err = html.ParseFragment(strings.NewReader(source), context)
		if err != nil {
			return source
		}
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			for i, attr := range node.Attr {
				if attr.Key != "href" {
					continue
				}
				target, err := url.Parse(attr.Val)
				if err != nil || target.Host != "" || target.Scheme != "" {
					continue
				}
				name := ""
				switch target.Path {
				case "/file", "/view/html":
					name = target.Query().Get("path")
				}
				if name == "" {
					continue
				}
				if doc, err := s.catalog.FindPath(name); err == nil {
					target.Path = "/"
					target.RawQuery = url.Values{"id": {doc.ID}}.Encode()
					node.Attr[i].Val = target.String()
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	var output bytes.Buffer
	for _, node := range nodes {
		walk(node)
		if err := html.Render(&output, node); err != nil {
			return source
		}
	}
	return output.String()
}
