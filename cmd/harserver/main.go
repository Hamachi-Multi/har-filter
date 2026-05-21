package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Hamachi-Multi/har-filter/internal/server"
)

const defaultListenAddress = "127.0.0.1:17680"

func main() {
	addr := flag.String("addr", defaultListenAddress, "HTTP listen address")
	maxUploadMB := flag.Int64("max-upload-mb", 200, "maximum HAR upload size in MiB")
	flag.Parse()

	if warning := listenExposureWarning(*addr); warning != "" {
		log.Print(warning)
	}

	app := server.New(server.Config{MaxUploadBytes: *maxUploadMB << 20})
	httpServer := newHTTPServer(*addr, app)

	log.Printf("HAR Filter listening on http://%s", *addr)
	log.Fatal(httpServer.ListenAndServe())
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       time.Minute,
	}
}

func listenExposureWarning(addr string) string {
	if isLoopbackListenAddress(addr) {
		return ""
	}
	return "Warning: binding outside loopback may expose HAR data to other devices; prefer 127.0.0.1 unless the network is trusted"
}

func isLoopbackListenAddress(addr string) bool {
	host := strings.TrimSpace(addr)
	if parsedHost, _, err := net.SplitHostPort(addr); err == nil {
		host = parsedHost
	}
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
