package backup

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// Vectores calculados con botocore (el firmador oficial de AWS para Python) con los mismos datos.
func TestFirmaS3CoincideConBotocore(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	sum := "38b6a9e161d980d317cdb6cbb9cbb1a89fa659ddc9f4504393669d01746d5343" // sha256("contenido cifrado de prueba")
	cases := []struct {
		endpoint, bucket, prefix, name string
		path                           bool
		wantURL, wantSig               string
	}{
		{"https://s3.example.com", "mi-bucket", "pulse/", "pulse-20261016T120000Z-v13.db.age", true,
			"https://s3.example.com/mi-bucket/pulse/pulse-20261016T120000Z-v13.db.age", "9ef731e94350f4e8c262014b2d26e8525b9fc593d7668202673e964f5636c6a3"},
		{"https://s3.us-east-1.amazonaws.com", "mi-bucket", "pulse/", "a b.age", false,
			"https://mi-bucket.s3.us-east-1.amazonaws.com/pulse/a%20b.age", "5c883c7fc8c3e5ad7ff40061ca0da28024563c6b5a39b82b8d7bdd67ae8cb173"},
	}
	for _, c := range cases {
		ep, _ := url.Parse(c.endpoint)
		s := &S3{Endpoint: ep, Region: "us-east-1", Bucket: c.bucket, Prefix: c.prefix, AccessKey: "AKIAEXAMPLE", SecretKey: "secretoDePrueba/123", PathStyle: c.path}
		req, err := s.request(c.name, strings.NewReader("contenido cifrado de prueba"), 27, sum, now)
		if err != nil {
			t.Fatal(err)
		}
		if req.URL.String() != c.wantURL {
			t.Errorf("URL %s, esperaba %s", req.URL, c.wantURL)
		}
		want := "AWS4-HMAC-SHA256 Credential=AKIAEXAMPLE/20261016/us-east-1/s3/aws4_request, SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date, Signature=" + c.wantSig
		if got := req.Header.Get("Authorization"); got != want {
			t.Errorf("firma:\n %s\nesperaba\n %s", got, want)
		}
		if strings.Contains(s.Describe(), "secreto") || strings.Contains(s.Describe(), "AKIA") {
			t.Error("la descripción no debe mostrar credenciales")
		}
	}
}
