package modelprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestImageDownloadBlocksPrivateAndRedirects(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://169.254.169.254/secret", 302)
			return
		}
		w.Write([]byte("image"))
	}))
	defer server.Close()
	if _, err := NewGatewayForwarder(nil).DownloadImage(context.Background(), server.URL); err == nil || hits != 0 {
		t.Fatal("未阻止私网图片")
	}
	f := NewGatewayForwarder([]string{"127.0.0.1"})
	if _, err := f.DownloadImage(context.Background(), server.URL+"/redirect"); err == nil || hits != 1 {
		t.Fatal("不应跟随图片重定向")
	}
	if data, err := f.DownloadImage(context.Background(), server.URL); err != nil || string(data) != "image" {
		t.Fatal(err)
	}
}
