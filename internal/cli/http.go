package cli

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
)

// environmentHTTPClient makes the conventional SSL_CERT_FILE override work on
// every supported OS. Go's system-pool implementation does not consume that
// variable consistently on macOS and Windows, while conformance and corporate
// repositories commonly use a consumer-owned private CA.
func environmentHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	certificateFile := os.Getenv("SSL_CERT_FILE")
	if certificateFile == "" {
		return &http.Client{Transport: transport}
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	pem, err := os.ReadFile(certificateFile)
	if err != nil || !pool.AppendCertsFromPEM(pem) {
		return &http.Client{Transport: transport}
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport}
}
