package mythicGraphql

import (
	"crypto/tls"
	"errors"
	"github.com/Khan/genqlient/graphql"
	"net/http"
)

type authedTransport struct {
	key     string
	wrapped http.RoundTripper
}

func (t *authedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("apitoken", t.key)
	if t.key == "" {
		return nil, errors.New("no auth token provided")
	}
	return t.wrapped.RoundTrip(req)
}

func NewClient(endpoint string, key string) graphql.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := http.Client{
		Transport: &authedTransport{
			key:     key,
			wrapped: tr,
		},
	}
	return graphql.NewClient(endpoint, &httpClient)
}
