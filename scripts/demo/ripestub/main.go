//go:build ignore

// ripestub answers the one RIPEstat call AS-Stats makes at startup
// (announced-prefixes) with documentation prefixes, so a demo stack gets a
// local-prefix scope without querying the real service.
//
// It creates a throwaway self-signed certificate for stat.ripe.net, writes it
// to $CERT_OUT (the API container points SSL_CERT_FILE at it) and serves TLS
// on :443. Give this container the network alias stat.ripe.net.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net/http"
	"os"
	"time"
)

const body = `{"status":"ok","data":{"resource":"AS64500","prefixes":[
{"prefix":"192.0.2.0/24"},{"prefix":"198.51.100.0/24"},
{"prefix":"203.0.113.0/24"},{"prefix":"2001:db8::/32"}]}}`

func main() {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "stat.ripe.net"},
		DNSNames:              []string{"stat.ripe.net"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(7 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		log.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	out := os.Getenv("CERT_OUT")
	if out == "" {
		out = "/stub/ca.pem"
	}
	if err := os.WriteFile(out, certPEM, 0o644); err != nil {
		log.Fatal(err)
	}
	pair := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}

	http.HandleFunc("/data/announced-prefixes/data.json", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("announced-prefixes %s", r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	srv := &http.Server{Addr: ":443", TLSConfig: &tls.Config{Certificates: []tls.Certificate{pair}}}
	log.Printf("ripestub: serving documentation prefixes, cert at %s", out)
	log.Fatal(srv.ListenAndServeTLS("", ""))
}
