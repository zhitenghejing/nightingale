//go:build linux

package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var fingerprintAsset = regexp.MustCompile(`^/assets/[^/]+[-.][a-zA-Z0-9_-]{8,}\.(js|css)$`)
var publicScript = regexp.MustCompile(`^/(js|font)/[^/]+\.js$`)

// Only public, content-addressed frontend assets survive a preview refresh in
// browser/CDN caches. HTML, authenticated APIs and error responses stay no-store.
func optimizeAssetResponse(response *http.Response) {
	request := response.Request
	fingerprinted := fingerprintAsset.MatchString(request.URL.Path)
	if (request.Method != http.MethodGet && request.Method != http.MethodHead) ||
		(!fingerprinted && !publicScript.MatchString(request.URL.Path)) || response.Header.Get("Set-Cookie") != "" {
		return
	}
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode != http.StatusNotModified &&
		((response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent) ||
			!(strings.HasPrefix(contentType, "text/javascript") || strings.HasPrefix(contentType, "application/javascript") || strings.HasPrefix(contentType, "text/css"))) {
		return
	}
	if fingerprinted {
		response.Header.Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	response.Header.Add("Vary", "Accept-Encoding")
	if request.Method != http.MethodGet || response.StatusCode != http.StatusOK ||
		request.Header.Get("Range") != "" || response.Header.Get("Content-Encoding") != "" ||
		!acceptsGzip(request.Header.Get("Accept-Encoding")) {
		return
	}
	original := response.Body
	reader, writer := io.Pipe()
	response.Body = &compressedAssetBody{PipeReader: reader, original: original}
	response.ContentLength = -1
	response.Header.Del("Content-Length")
	response.Header.Set("Content-Encoding", "gzip")
	if etag := response.Header.Get("ETag"); etag != "" && !strings.HasPrefix(etag, "W/") {
		response.Header.Set("ETag", "W/"+etag)
	}
	go func() {
		defer original.Close()
		compressed := gzip.NewWriter(writer)
		_, err := io.Copy(compressed, original)
		if closeErr := compressed.Close(); err == nil {
			err = closeErr
		}
		writer.CloseWithError(err)
	}()
}

type compressedAssetBody struct {
	*io.PipeReader
	original io.Closer
}

func (body *compressedAssetBody) Close() error {
	// A disconnected browser must also unblock the compressor and upstream read.
	err := body.PipeReader.Close()
	body.original.Close()
	return err
}

func acceptsGzip(header string) bool {
	for _, coding := range strings.Split(header, ",") {
		parts := strings.Split(coding, ";")
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "gzip") {
			continue
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			name, value, ok := strings.Cut(parameter, "=")
			if ok && strings.EqualFold(strings.TrimSpace(name), "q") {
				parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if err != nil || parsed < 0 || parsed > 1 {
					return false
				}
				quality = parsed
			}
		}
		return quality > 0
	}
	return false
}
